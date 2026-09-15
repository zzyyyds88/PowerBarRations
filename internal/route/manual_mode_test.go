package route

import (
	"net/http"
	"testing"

	"pbr/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// §2.1 manual 不参与冷却：指定成员冷却中仍应可用（否则冷却期内全部 503）。
func TestManualModeIgnoresCooldown(t *testing.T) {
	resolved := testRoute("manual-nocool", 2, 1)
	resolved.Mode = model.LaneModeManual
	resolved.ActiveMember = "channel-a/model-1"
	st := NewState(resolved)

	st.Runtime.withLock(func() {
		st.Runtime.Cooldowns[memberKeyOf(&resolved.Members[0])] = nowMs() + 60_000
	})
	member, _, ok := st.Next(nil)
	require.True(t, ok, "§2.1：manual 成员冷却中仍应可用（不参与冷却）")
	assert.Equal(t, "channel-a", memberName(member))
}

// §2.1 manual 仍过熔断：熔断打开且退避未到 → 无可用。
func TestManualModeHonorsCircuit(t *testing.T) {
	resolved := testRoute("manual-circuit", 2, 1)
	resolved.Mode = model.LaneModeManual
	resolved.ActiveMember = "channel-a/model-1"
	st := NewState(resolved)

	st.Runtime.withLock(func() {
		st.Runtime.Circuits[memberKeyOf(&resolved.Members[0])] = &Circuit{
			State: CircuitOpen, OpenUntil: nowMs() + 60_000,
		}
	})
	_, _, ok := st.Next(nil)
	assert.False(t, ok, "§2.1：manual 熔断打开应返回无可用")
}

// failover 仍必须尊重冷却（不能把 manual 的豁免误用到其它模式）。
func TestFailoverStillHonorsCooldown(t *testing.T) {
	resolved := testRoute("failover-cool", 2, 1)
	st := NewState(resolved)

	st.Runtime.withLock(func() {
		st.Runtime.Cooldowns[memberKeyOf(&resolved.Members[0])] = nowMs() + 60_000
	})
	member, _, ok := st.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "failover 应跳过冷却中的 ch1")
}

// §6：亲和期内当前成员失败 → 立即结束亲和。
//
// 参考实现（reference/octopus-bestrui groupRouteLocked 的 recordRouteFailure）
// 在 CurrentItemID == itemID 时显式 `AffinityUntil = 0`。此前 PBR 的
// recordFailure 完全不碰亲和状态，失败成员会在剩余亲和窗口内继续被粘滞，
// 形成"失败 → 仍粘着 → 再失败"。
func TestAffinityEndsWhenCurrentMemberFails(t *testing.T) {
	resolved := testRoute("affinity-fail", 2, 1)
	resolved.Config.MemberAffinitySeconds = 300
	state := NewState(resolved)

	// ch1 → 失败 → ch2（武装亲和）→ 成功（启动亲和）
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	member, _, ok := state.Next(apiError(http.StatusInternalServerError, "", false))
	require.True(t, ok)
	require.Equal(t, "channel-b", memberName(member))
	state.OnSuccess()

	state.Runtime.withLock(func() {
		require.Greater(t, state.Runtime.AffinityUntil, nowMs(), "前提：亲和已启动")
	})

	// 亲和期内当前成员（ch2）失败
	_, _, _ = state.Next(apiError(http.StatusInternalServerError, "", false))

	state.Runtime.withLock(func() {
		assert.LessOrEqual(t, state.Runtime.AffinityUntil, nowMs(),
			"§6：亲和期内当前成员失败必须立即结束亲和")
	})
}

// ---------- rolling_success_rate（routing-spec §7） ----------

// health 快照必须含 rolling_success_rate，且随成败变化。
func TestRollingSuccessRateInHealthSnapshot(t *testing.T) {
	resolved := testRoute("rolling-rate", 1, 1)
	state := NewState(resolved)
	key := memberKeyOf(&resolved.Members[0])

	// 无样本时：0
	snapshot := state.Runtime.Health(resolved, CurrentCircuitSettings())
	require.Len(t, snapshot.Members, 1)
	assert.Equal(t, 0.0, snapshot.Members[0].RollingSuccessRate,
		"无样本时 rolling_success_rate 应为 0")

	// 1 次成功 → 1.0
	state.Runtime.withLock(func() { state.Runtime.recordSuccess(key, 0) })
	snapshot = state.Runtime.Health(resolved, CurrentCircuitSettings())
	assert.Equal(t, 1.0, snapshot.Members[0].RollingSuccessRate)

	// 成功 + 失败 → 0.5
	state.Runtime.withLock(func() {
		state.Runtime.recordFailure(key, KindSoftTransient, 0, CurrentCircuitSettings())
	})
	snapshot = state.Runtime.Health(resolved, CurrentCircuitSettings())
	assert.InDelta(t, 0.5, snapshot.Members[0].RollingSuccessRate, 0.001)
}

// 滚动窗口必须有界：连打超过窗口大小的失败不会让缓冲无限增长。
func TestRollingWindowIsBounded(t *testing.T) {
	circuit := &Circuit{State: CircuitClosed}
	for i := 0; i < rollingWindowSize*5; i++ {
		circuit.recordOutcome(false)
	}
	assert.LessOrEqual(t, len(circuit.RecentOutcomes), rollingWindowSize,
		"滚动窗口必须有界（否则长期运行会无界增长）")
	assert.Equal(t, 0.0, circuit.RollingSuccessRate())
}
