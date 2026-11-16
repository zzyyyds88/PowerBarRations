package route

// 成员级人工停用（LaneMember.Disabled → RouteMember.Disabled）的选路语义。
// 口径真源：routing-spec §1.2、§1.3、§2.1、§2.2、§5.3、§7、§9；用例清单见 test-spec §3.1。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zzyyyds88/PowerBarRations/model"
)

// failover 跳过被关闭成员，并在 attempts 留痕 `disabled`（不得复用 cooldown/skipped）。
func TestFailoverSkipsDisabledMemberAndRecordsDisabledStatus(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-disable-failover", 2, 1)
	resolved.Members[0].Disabled = true
	state := NewState(resolved)

	member, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "被关闭的 ch1 必须被跳过")

	statuses := map[string]string{}
	for _, a := range state.History() {
		statuses[a.Member] = a.Status
	}
	assert.Equal(t, "disabled", statuses["channel-a/model-1"],
		"人工停用必须留痕为 disabled，与 cooldown/circuit_break/skipped 可区分")
}

// manual 的 active_member 被关闭 → 无可用，绝不静默换人。
func TestManualDisabledActiveMemberReturnsUnavailable(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-disable-manual", 2, 2)
	resolved.Mode = model.LaneModeManual
	resolved.ActiveMember = "channel-a/model-1"
	resolved.Members[0].Disabled = true
	state := NewState(resolved)

	_, _, ok := state.Next(nil)
	assert.False(t, ok, "manual 指定的成员被关闭即无可用，不得换成 channel-b")

	statuses := map[string]string{}
	for _, a := range state.History() {
		statuses[a.Member] = a.Status
	}
	assert.Equal(t, "disabled", statuses["channel-a/model-1"], "manual 的停用同样要留痕")
}

// 跳过被关闭成员不得写冷却、不得建立熔断器——它是配置态结论，不产生运行态副作用。
func TestDisablingMemberWritesNoRuntimeState(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-disable-no-runtime", 2, 1)
	resolved.Members[0].Disabled = true
	state := NewState(resolved)

	_, _, ok := state.Next(nil)
	require.True(t, ok)

	key := memberKeyOf(&resolved.Members[0])
	state.Runtime.withLock(func() {
		assert.Zero(t, state.Runtime.Cooldowns[key], "跳过停用成员不得写冷却")
		assert.Nil(t, state.Runtime.Circuits[key], "跳过停用成员不得建立熔断器")
	})
}

// 快照签名必须对停用字段敏感（routing-spec §1.3）。夹具的 LaneVersion 与 MemberId
// 恒为 0，因此这里两串的差异只可能来自 Disabled 本身——正是这条契约要保护的场景：
// 不能靠"整体替换让主键变化"这类副作用驱动快照重建。
func TestSnapshotSignatureSensitiveToDisabled(t *testing.T) {
	resolved := testRoute("lane-disable-signature", 2, 2)
	before := routeSnapshotSignature(resolved)

	resolved.Members[1].Disabled = true
	after := routeSnapshotSignature(resolved)

	assert.NotEqual(t, before, after, "只改成员停用态也必须让快照签名变化")
}

// 被关闭成员绝不占用每轮唯一的半开探测槽，否则一个关着的成员会饿死整条链的恢复探测。
func TestDisabledMemberNeverClaimsProbeSlot(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-disable-probe", 2, 2)
	// ch1 优先级更高且冷却已到期（本是探测候选），但它被人工停用；
	// ch2 同样到探测窗口，应由 ch2 取得槽位。
	resolved.Members[0].Disabled = true
	state := NewState(resolved)
	key1 := memberKeyOf(&resolved.Members[0])
	key2 := memberKeyOf(&resolved.Members[1])
	state.Runtime.withLock(func() {
		state.Runtime.Cooldowns[key1] = nowMs() - 1
		state.Runtime.Cooldowns[key2] = nowMs() - 1
	})

	member, _, ok := state.Next(nil)
	require.True(t, ok)
	require.Equal(t, "channel-b", memberName(member))

	state.Runtime.withLock(func() {
		assert.Equal(t, key2, state.Runtime.ProbeMember, "探测槽必须落在启用的成员上")
	})
}

// 关闭当前成员即打破亲和，立即按顺序重选，而不是等亲和窗口到期。
func TestDisablingCurrentMemberBreaksAffinityImmediately(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-disable-affinity", 2, 2)
	state := NewState(resolved)
	key1 := memberKeyOf(&resolved.Members[0])

	// 先让 ch1 成为当前成员并处在亲和期内。
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	require.Equal(t, "channel-a", memberName(state.Current()))
	state.Runtime.withLock(func() {
		state.Runtime.HasCurrent = true
		state.Runtime.CurrentMember = key1
		state.Runtime.AffinityUntil = nowMs() + 60_000
	})

	// 亲和期内人工关掉 ch1：下一次选路必须立即离开它。直接改快照成员的配置态字段，
	// 等价于"保存车道后重建出的新快照里该成员是停用"（真实路径由签名触发重建）。
	state.Route.Members[0].Disabled = true
	member, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "被关闭的当前成员必须立即失效")
}

// 全部成员被关闭：成员链仍解析得到（停用≠删除），但选路无可用。
func TestAllMembersDisabledKeepsChainButYieldsNoMember(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-disable-all", 2, 2)
	for i := range resolved.Members {
		resolved.Members[i].Disabled = true
	}
	state := NewState(resolved)
	require.Len(t, resolved.Members, 2, "被关闭的成员仍留在成员链里")

	_, _, ok := state.Next(nil)
	assert.False(t, ok, "全部成员被关闭即无可用")

	history := state.History()
	require.Len(t, history, 2, "两名被关闭的成员都要留痕")
	for _, a := range history {
		assert.Equal(t, "disabled", a.Status)
	}
}

// 健康快照：停用成员 enabled=false 且 available=false，但运行态字段照常如实给出，
// 不归零也不省略——排障要能同时看到"我把它关了"和"关之前它是什么状态"。
func TestHealthSnapshotReportsDisabledWithoutErasingRuntime(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-disable-health", 2, 2)
	resolved.Members[0].Disabled = true
	state := NewState(resolved)
	key1 := memberKeyOf(&resolved.Members[0])
	state.Runtime.withLock(func() {
		state.Runtime.Circuits[key1] = &Circuit{
			State:               CircuitOpen,
			OpenUntil:           nowMs() + 60_000,
			ConsecutiveFailures: 3,
		}
		state.Runtime.Cooldowns[key1] = nowMs() + 30_000
	})

	snapshot := state.Runtime.Health(resolved, CurrentCircuitSettings())
	require.Len(t, snapshot.Members, 2)

	disabled := snapshot.Members[0]
	assert.False(t, disabled.Enabled, "停用成员 enabled=false")
	assert.False(t, disabled.Available, "停用成员 available=false")
	assert.Equal(t, CircuitOpen, disabled.Circuit, "熔断状态照常如实给出")
	assert.Equal(t, 3, disabled.ConsecutiveFailures, "熔断计数不因人工关闭而归零")
	assert.NotNil(t, disabled.CooldownUntil, "冷却时间不因人工关闭而省略")

	enabled := snapshot.Members[1]
	assert.True(t, enabled.Enabled, "未停用成员 enabled=true（反向字段零值安全）")
	assert.True(t, enabled.Available)
}
