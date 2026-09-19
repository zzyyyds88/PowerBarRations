package route

import (
	"testing"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 运行态注册表的有界性（routing-spec §1.3）：未配车道的模型名
// （Source=unconfigured、成员为空）不得进入进程级注册表。否则任何已鉴权请求
// 用一个任意模型名都会永久新增 Runtime，SnapshotLanes 随"请求过的模型名数"
// 无界增长，buildRouteStateSnapshot 退化成 O(请求过的模型名数) 次 DB 查询。
func TestUnconfiguredModelDoesNotRegisterRuntime(t *testing.T) {
	unconfigured := &model.ResolvedRoute{
		Model:    "ghost-model-unconfigured",
		RouteKey: "ghost-model-unconfigured",
		Source:   model.RouteSourceUnconfigured,
		Mode:     model.LaneModeFailover,
		Config:   model.DefaultLaneRelayConfig(),
		Members:  []model.RouteMember{},
	}

	for i := 0; i < 50; i++ {
		state := NewState(unconfigured)
		require.NotNil(t, state.Runtime, "临时运行态不能为 nil（Next/收尾路径要解引用）")
		_, _, ok := state.Next(nil)
		assert.False(t, ok, "空链必须立即判为无可用")
	}

	assert.Nil(t, Default.Get(unconfigured.RouteKey),
		"未配车道的模型名不得进入注册表（否则逐请求无界堆积）")
	assert.NotContains(t, Default.SnapshotLanes(), unconfigured.RouteKey)
}

// 非显式来源即使恰好带了成员也不登记：注册表的唯一合法来源是显式车道。
func TestNonExplicitRouteDoesNotRegisterRuntime(t *testing.T) {
	resolved := testRoute("lane-non-explicit-registry", 1, 1)
	resolved.Source = model.RouteSourceUnconfigured

	NewState(resolved)
	assert.Nil(t, Default.Get(laneKeyOf(resolved)),
		"非显式来源不得登记运行态")
}

// 显式且成员非空的车道必须登记，并且重复请求复用同一份运行态。
func TestExplicitLaneRegistersAndReusesRuntime(t *testing.T) {
	resolved := testRoute("lane-register-once", 1, 1)
	first := NewState(resolved)
	require.NotNil(t, Default.Get(laneKeyOf(resolved)))
	assert.Same(t, first.Runtime, NewState(resolved).Runtime,
		"同一车道必须复用同一份运行态（冷却/熔断跨请求共享）")
	assert.Contains(t, Default.SnapshotLanes(), laneKeyOf(resolved))
}

// 删除车道后移除运行态：否则 SnapshotLanes 会一直保留已不存在的车道，
// 快照每轮都为它解析一次路由（routing-spec §1.3）。
func TestRegistryRemoveDropsRuntime(t *testing.T) {
	resolved := testRoute("lane-remove-test", 1, 1)
	NewState(resolved)
	require.NotNil(t, Default.Get(laneKeyOf(resolved)))

	Default.Remove(laneKeyOf(resolved))
	assert.Nil(t, Default.Get(laneKeyOf(resolved)))
	assert.NotContains(t, Default.SnapshotLanes(), laneKeyOf(resolved))

	// 幂等：再次删除不 panic。
	Default.Remove(laneKeyOf(resolved))
}
