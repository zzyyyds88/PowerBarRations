package route

import (
	"errors"
	"net/http"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routing-spec §3 第 4 步：尝试循环"每轮重新读取车道配置，支持热更新"。
// 运行中给车道加成员/改六键，下一轮尝试即应看到新快照。

// 第二轮开始前重读配置：新加入的成员可被选中。
func TestNextReloadsLaneConfigBetweenRounds(t *testing.T) {
	withFakeClock(t)

	// 旧快照：只有 channel-a。
	resolved := testRoute("lane-hot-reload", 2, 2)
	old := *resolved
	old.Members = resolved.Members[:1]

	// 重读返回"新增 channel-b"的新快照（成员键不同，签名必变）。
	updated := *resolved
	reloadCalls := 0
	original := resolveRouteForReload
	resolveRouteForReload = func(name string) (*model.ResolvedRoute, error) {
		reloadCalls++
		assert.Equal(t, "lane-hot-reload", name, "重读应使用当前路由键")
		return &updated, nil
	}
	t.Cleanup(func() { resolveRouteForReload = original })

	state := NewState(&old)
	member, _, ok := state.Next(nil)
	require.True(t, ok)
	require.Equal(t, "channel-a", memberName(member))
	assert.Zero(t, reloadCalls, "首轮不重读：NewState 的解析就是本轮读取")

	// 硬鉴权故障不原地重试 → 进入下一轮前重读配置。
	member, _, ok = state.Next(apiError(http.StatusUnauthorized, "", false))
	require.True(t, ok)
	assert.Equal(t, 1, reloadCalls, "新一轮开始前必须重读一次")
	assert.Equal(t, "channel-b", memberName(member), "重读后新成员应可被选中")
}

// 重读失败（DB 抖动）保留旧快照，不得让瞬时故障改变路由。
func TestNextReloadFailureKeepsPreviousSnapshot(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-hot-reload-err", 2, 1)

	original := resolveRouteForReload
	resolveRouteForReload = func(string) (*model.ResolvedRoute, error) {
		return nil, errors.New("db down")
	}
	t.Cleanup(func() { resolveRouteForReload = original })

	state := NewState(resolved)
	member, _, ok := state.Next(nil)
	require.True(t, ok)
	require.Equal(t, "channel-a", memberName(member))

	// 重读失败 → 仍按旧快照换到 channel-b（而不是变成"无可用"）。
	member, _, ok = state.Next(apiError(http.StatusUnauthorized, "", false))
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "重读失败必须保留旧快照继续尝试")
}

// §2.1 manual 车道失败不得写冷却（否则唯一成员被自己的冷却挡住，冷却期满前全部 503）；
// 熔断计分照常。
func TestManualModeDoesNotWriteCooldownOnFailure(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-manual-no-cooldown", 2, 1)
	resolved.Mode = model.LaneModeManual
	resolved.ActiveMember = "channel-a/model-1"
	state := NewState(resolved)

	member, _, ok := state.Next(nil)
	require.True(t, ok)
	require.Equal(t, "channel-a", memberName(member))

	_, _, ok = state.Next(apiError(http.StatusUnauthorized, "", false))
	assert.False(t, ok, "manual 唯一成员失败后无可用，不静默换人")

	key := memberKeyOf(&resolved.Members[0])
	state.Runtime.withLock(func() {
		assert.Zero(t, state.Runtime.Cooldowns[key], "manual 失败不得写冷却")
		assert.NotNil(t, state.Runtime.Circuits[key], "熔断计分照常")
	})
}

// 对照：failover 车道失败必须写冷却，避免把 manual 的豁免误用到其它模式。
func TestFailoverModeWritesCooldownOnFailure(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-failover-cooldown", 2, 1)
	state := NewState(resolved)

	_, _, ok := state.Next(nil)
	require.True(t, ok)
	_, _, ok = state.Next(apiError(http.StatusUnauthorized, "", false))
	require.True(t, ok)

	state.Runtime.withLock(func() {
		assert.Greater(t, state.Runtime.Cooldowns[memberKeyOf(&resolved.Members[0])], nowMs(),
			"failover 失败必须写冷却")
	})
}

// ReportMemberUnavailable 也走 §2.1 的"manual 不写冷却"分支。
func TestManualReportMemberUnavailableSkipsCooldown(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-manual-report", 2, 1)
	resolved.Mode = model.LaneModeManual
	resolved.ActiveMember = "channel-a/model-1"
	state := NewState(resolved)

	member, _, ok := state.Next(nil)
	require.True(t, ok)
	state.ReportMemberUnavailable(member, "test")

	state.Runtime.withLock(func() {
		assert.Zero(t, state.Runtime.Cooldowns[memberKeyOf(&resolved.Members[0])])
	})
}
