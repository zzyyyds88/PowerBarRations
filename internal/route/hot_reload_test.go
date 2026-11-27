package route

import (
	"net/http"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ADR 0014：一次请求只消费 PBRServe 入口生成的不可变路由快照，重试不得
// 重新解析车道或替换成员链。
func TestNextKeepsImmutableSnapshotBetweenRounds(t *testing.T) {
	withFakeClock(t)

	resolved := testRoute(t, "lane-immutable-snapshot", 2, 2)
	old := *resolved
	old.Members = resolved.Members[:1]
	old.Config.MemberMaxAttempts = 1

	state := NewState(&old)
	member, _, ok := state.Next(nil)
	require.True(t, ok)
	require.Equal(t, "channel-a", memberName(member))

	// 硬鉴权故障不原地重试；旧快照只有 channel-a，因此没有新成员可选。
	member, _, ok = state.Next(apiError(http.StatusUnauthorized, "", false))
	assert.False(t, ok)
	assert.Nil(t, member)
	assert.Same(t, &old, state.Route)
	assert.Len(t, state.Route.Members, 1)
}

func TestStateAdmissionDeadlineSetOnce(t *testing.T) {
	state := NewState(testRoute(t, "lane-admission-deadline", 1, 1))
	first := time.Unix(100, 0)
	second := time.Unix(200, 0)
	state.SetAdmissionDeadline(first)
	state.SetAdmissionDeadline(second)
	assert.Equal(t, first, state.AdmissionDeadline())
}

func TestNextDoesNotReplaceRuntimeOrGeneration(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-immutable-runtime", 2, 1)
	state := NewState(resolved)
	runtime := state.Runtime
	generation := state.runtimeGeneration

	_, _, ok := state.Next(nil)
	require.True(t, ok)
	_, _, ok = state.Next(apiError(http.StatusUnauthorized, "", false))
	require.True(t, ok)

	assert.Same(t, runtime, state.Runtime)
	assert.Equal(t, generation, state.runtimeGeneration)
	assert.Same(t, resolved, state.Route)
}

// §2.1 manual 车道失败不得写冷却（否则唯一成员被自己的冷却挡住，冷却期满前全部 503）；
// 熔断计分照常。
func TestManualModeDoesNotWriteCooldownOnFailure(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-manual-no-cooldown", 2, 1)
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
	resolved := testRoute(t, "lane-failover-cooldown", 2, 1)
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
	resolved := testRoute(t, "lane-manual-report", 2, 1)
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
