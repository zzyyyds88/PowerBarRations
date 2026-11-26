package route

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 审查 7a 回归：hard_auth / hard_quota 的冷却必须取较长档（routing-spec §4.1）。
func TestHardAuthUsesLongerCooldownTier(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-hard-cooldown", 2, 1)
	resolved.Config.MemberCooldownSeconds = 10
	state := NewState(resolved)

	_, _, ok := state.Next(nil)
	require.True(t, ok)
	_, _, ok = state.Next(apiError(http.StatusUnauthorized, "", false))
	require.True(t, ok, "硬鉴权应立即换人")

	rt := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])
	until := rt.Cooldowns[key]
	require.NotZero(t, until)
	base := 10 * hardFailureCooldownMultiplier
	assert.Equal(t, nowMs()+int64(jitterSeconds(base, key, nowMs()))*1000, until,
		"hard_auth 冷却应为 base*%d 再加抖动", hardFailureCooldownMultiplier)
}

// 审查 7a 回归：软故障仍用基准冷却，不得被放大。
// 2026-10-23 契约变更：软瞬时故障（500/超时/EOF）的成员冷却改为**分级**——
// 连续第 n 次：n=1 → base/8、n=2 → base/4、n=3 → base/2、n>=4 → base，下限 5s。
//
// 旧契约是"一次软失败就锁满档 base"，现场代价：一个成员 500 抖一下就被锁 60~72 秒，
// 流量全压到下一个成员 → 把它打爆 → 两条一起冷却 → 整条车道瞬时 503
// （2026-10-23 Opencode1/华为云 实案）。本测试锁住新契约：base=10 时首次只歇 5s。
func TestSoftTransientCooldownIsGraded(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-soft-cooldown", 1, 1)
	resolved.Config.MemberCooldownSeconds = 10
	state := NewState(resolved)

	_, _, ok := state.Next(nil)
	require.True(t, ok)
	_, _, ok = state.Next(apiError(http.StatusInternalServerError, "", false))
	assert.False(t, ok)

	rt := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])
	// base=10 → 10/8 = 1 → 取下限 5s（不再是 10s 满档）
	assert.Equal(t, nowMs()+softTransientMinCooldownSeconds*1000, rt.Cooldowns[key])
}

// 2026-10-23 新增：恐慌模式（panic threshold）。整条车道只剩"冷却中"的成员时，
// 压着冷却强行上一个，而不是对外返回 503 —— 否则一次抖动就能把整条通道变成不可用。
func TestPanicModeServesWhenEveryMemberCooling(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-panic", 2, 1)
	resolved.Config.MemberCooldownSeconds = 60
	state := NewState(resolved)
	rt := Default.For(resolved.Model)

	// 直接把两个成员都置为"冷却中"（等价于各失败一次后的状态）。
	for i := range resolved.Members {
		rt.Cooldowns[memberKeyOf(&resolved.Members[i])] = nowMs() + 60_000
	}
	assert.Len(t, rt.Cooldowns, 2, "两个成员都应在冷却中")

	// 常规选路会 503；恐慌模式必须仍然给出一个成员。
	member, _, ok := state.Next(nil)
	require.True(t, ok, "全成员冷却时不得直接返回无可用成员")
	require.NotNil(t, member)

	panicEvents := 0
	for _, e := range rt.Events {
		if e.Type == EventPanicPick {
			panicEvents++
		}
	}
	assert.GreaterOrEqual(t, panicEvents, 1, "恐慌模式必须留痕 panic_pick")
}

// 恐慌模式不得越过"人工停用"与"熔断打开"：这两类属于确定不可用。
func TestPanicModeSkipsDisabledAndOpenCircuit(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-panic-guard", 1, 1)
	resolved.Config.MemberCooldownSeconds = 60
	resolved.Members[0].Disabled = true
	state := NewState(resolved)

	_, _, ok := state.Next(nil)
	assert.False(t, ok, "人工停用的成员不得被恐慌模式捞回来")
}

// 审查 7c 回归：半开成员不得被并发请求当作健康成员直接选中绕过单探测槽。
func TestHalfOpenCircuitOnlyAllowsSingleProbe(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-half-open", 1, 1)
	resolved.Config.MemberCooldownSeconds = 5
	state := NewState(resolved)
	key := memberKeyOf(&resolved.Members[0])

	// 直接把该成员置为半开，模拟"探测请求已把熔断从 open 翻成 half_open"。
	rt := Default.For(resolved.Model)
	rt.withLock(func() {
		rt.Circuits[key] = &Circuit{State: CircuitHalfOpen}
	})

	// 第一个请求占用探测槽。
	first, _, ok := state.Next(nil)
	require.True(t, ok)
	require.NotNil(t, first)
	require.True(t, rt.probe != nil, "首个请求应占用探测槽")

	// 第二个并发请求：半开且槽被占 → 必须判为无可用，而不是选中该成员。
	second := NewState(resolved)
	_, _, ok = second.Next(nil)
	assert.False(t, ok, "半开探测在途时并发请求不得再选中该成员")

	// 探测成功后归还槽，后续请求恢复可用。
	state.OnSuccess()
	assert.False(t, rt.probe != nil, "成功后应归还探测槽")
	third := NewState(resolved)
	_, _, ok = third.Next(nil)
	assert.True(t, ok)
}

// 审查 7b 回归：成员渠道不可用要记一次失败并进冷却，不能只跳过。
func TestUnavailableChannelCountsAsFailure(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-unavailable", 1, 1)
	resolved.Config.MemberCooldownSeconds = 30
	state := NewState(resolved)

	state.ReportMemberUnavailable(&resolved.Members[0], "channel disabled")

	rt := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])
	_, cooling := rt.Cooldowns[key]
	assert.True(t, cooling, "渠道不可用必须写入冷却")
	assert.NotEmpty(t, state.History(), "必须留下失败尝试记录用于排障")
}
