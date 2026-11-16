package route

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routing-spec §1.3：成员被删除时清理其残留状态（参照线上 groupRouteLocked）。

// 成员删除后重新加回同一 (channel_id, upstream_model) 会复用同一个运行态键，
// 若不清残留，新成员会直接继承旧的冷却/熔断。
func TestStaleStatePrunedWhenMemberRemoved(t *testing.T) {
	resolved := testRoute("stale-prune", 2, 1)
	state := NewState(resolved)
	key := memberKeyOf(&resolved.Members[0])

	state.Runtime.withLock(func() {
		state.Runtime.Cooldowns[key] = nowMs() + 3_600_000
		state.Runtime.Circuits[key] = &Circuit{State: CircuitOpen, OpenUntil: nowMs() + 3_600_000}
	})

	// 该成员从车道移除后重新解析
	resolved.Members = resolved.Members[1:]
	NewState(resolved)

	state.Runtime.withLock(func() {
		_, hasCooldown := state.Runtime.Cooldowns[key]
		_, hasCircuit := state.Runtime.Circuits[key]
		assert.False(t, hasCooldown, "删除成员的冷却状态必须被清理（否则重建同键会继承）")
		assert.False(t, hasCircuit, "删除成员的熔断状态必须被清理")
	})
}

// 探测槽被占用时成员被删除：槽必须归还，否则整条车道再也无法放行探测。
func TestProbeSlotReleasedWhenOwningMemberRemoved(t *testing.T) {
	resolved := testRoute("stale-probe", 2, 1)
	state := NewState(resolved)
	key := memberKeyOf(&resolved.Members[0])

	state.Runtime.withLock(func() { state.Runtime.takeProbe(key) })
	require.True(t, state.Runtime.HasProbe, "前提：探测槽已被占用")

	resolved.Members = resolved.Members[1:]
	NewState(resolved)

	state.Runtime.withLock(func() {
		assert.False(t, state.Runtime.HasProbe,
			"成员被删除后探测槽必须归还（否则该车道永久无法探测）")
	})
}

// 反向守卫：解析结果为空（模型暂时没有可用渠道）时**不得**清空状态，
// 否则渠道恢复后冷却/熔断的退避保护会丢失。
func TestStaleStateKeptWhenNoMembersResolved(t *testing.T) {
	resolved := testRoute("stale-keep", 1, 1)
	state := NewState(resolved)
	key := memberKeyOf(&resolved.Members[0])

	state.Runtime.withLock(func() {
		state.Runtime.Cooldowns[key] = nowMs() + 3_600_000
	})

	empty := testRoute("stale-keep", 0, 1)
	NewState(empty)

	state.Runtime.withLock(func() {
		_, stillThere := state.Runtime.Cooldowns[key]
		assert.True(t, stillThere,
			"成员集合为空时不得清空状态（否则渠道恢复后失去退避保护）")
	})
}

// 正常路径不受影响：仍在成员链里的冷却状态必须保留。
func TestStaleStateKeepsActiveMembers(t *testing.T) {
	resolved := testRoute("stale-active", 2, 1)
	state := NewState(resolved)
	key := memberKeyOf(&resolved.Members[0])

	state.Runtime.withLock(func() {
		state.Runtime.Cooldowns[key] = nowMs() + 3_600_000
	})
	NewState(resolved)

	state.Runtime.withLock(func() {
		_, stillThere := state.Runtime.Cooldowns[key]
		assert.True(t, stillThere, "仍在成员链里的冷却状态不得被清理")
	})
}
