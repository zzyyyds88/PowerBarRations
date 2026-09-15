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
	assert.Equal(t, nowMs()+int64(10*hardFailureCooldownMultiplier)*1000, until,
		"hard_auth 冷却应为 base*%d", hardFailureCooldownMultiplier)
}

// 审查 7a 回归：软故障仍用基准冷却，不得被放大。
func TestSoftFailureKeepsBaseCooldown(t *testing.T) {
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
	assert.Equal(t, nowMs()+10*1000, rt.Cooldowns[key])
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
	require.True(t, rt.HasProbe, "首个请求应占用探测槽")

	// 第二个并发请求：半开且槽被占 → 必须判为无可用，而不是选中该成员。
	second := NewState(resolved)
	_, _, ok = second.Next(nil)
	assert.False(t, ok, "半开探测在途时并发请求不得再选中该成员")

	// 探测成功后归还槽，后续请求恢复可用。
	state.OnSuccess()
	assert.False(t, rt.HasProbe, "成功后应归还探测槽")
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
