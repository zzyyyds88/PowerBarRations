package route

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// 429 的 Retry-After 超过本地上限时，State.Next 必须中止本轮并交回原错误，
// 由 controller 原样透传 429；不得等待、不得换人、不得合成 503。
func TestRetryAfterOverCapAbortsWithoutWaiting(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-429-abort", 2, 2)
	state := NewState(resolved)
	first, _, ok := state.Next(nil)
	require.True(t, ok)

	rateErr := apiError(http.StatusTooManyRequests, types.ErrorCode(""), false)
	rateErr.RetryAfterSeconds = rateLimitMaxPaceSeconds + 1
	_, wait, ok := state.Next(rateErr)
	assert.False(t, ok, "超 cap 的 429 必须中止本轮")
	assert.Zero(t, wait, "不得返回等待时长")
	assert.Same(t, rateErr, state.AbortError())

	state.Runtime.withLock(func() {
		key := memberKeyOf(first)
		assert.Greater(t, state.Runtime.PacedUntil[key], int64(0),
			"仍需为后续请求记录受限减速，但本请求不等待")
	})
}

// timeout 必须一次性耗尽该成员在本请求内的预算：即使短冷却到期，
// 同一请求也不能再次选中它（30s×2 的 200s 串行链回归）。
func TestTimeoutConsumesMemberBudgetForRequest(t *testing.T) {
	clock := withFakeClock(t)
	resolved := testRoute(t, "lane-timeout-budget", 2, 2)
	state := NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)

	timeoutErr := apiError(http.StatusGatewayTimeout, types.ErrorCodeChannelResponseTimeExceeded, false)
	_, _, ok = state.Next(timeoutErr)
	require.True(t, ok, "第二个成员仍应被尝试")
	_, _, ok = state.Next(timeoutErr)
	require.False(t, ok, "第二个成员预算耗尽后本轮应结束")

	clock.advance(30 * time.Second) // 所有短冷却都已到期
	_, _, ok = state.Next(nil)
	assert.False(t, ok, "同一请求内不得重选已 timeout 的成员")
}

// FailsByKind 必须是严格“连续”：timeout 与 soft_transient 交替时不能
// 让任一类型的计数继续累加、导致比允许次数更早开断。
func TestFailsByKindResetsOnDifferentFailure(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-fails-by-kind", 1, 1)
	runtime := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])
	settings := DefaultCircuitSettings()

	seq := []ErrorKind{
		KindTimeout, KindSoftTransient,
		KindTimeout, KindSoftTransient,
		KindTimeout, KindSoftTransient,
		KindTimeout,
	}
	for _, kind := range seq {
		runtime.withLock(func() {
			runtime.recordFailure(key, kind, 60, 0, true, settings)
		})
	}

	runtime.withLock(func() {
		circuit := runtime.Circuits[key]
		require.NotNil(t, circuit)
		assert.Equal(t, CircuitClosed, circuit.State,
			"交替失败不得触发 allowed_fails 开断")
		assert.Equal(t, 1, circuit.FailsByKind[KindTimeout])
		assert.Zero(t, circuit.FailsByKind[KindSoftTransient])
	})
}

// 冷却/减速只能被延长，后到的短失败不得覆盖先前的长冷却。
func TestCooldownAndPaceAreMonotonic(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-monotonic", 1, 1)
	runtime := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])
	settings := DefaultCircuitSettings()

	runtime.withLock(func() {
		runtime.recordFailure(key, KindHardAuth, 60, 0, true, settings)
		hard := runtime.Cooldowns[key]
		runtime.recordFailure(key, KindSoftTransient, 60, 0, true, settings)
		assert.GreaterOrEqual(t, runtime.Cooldowns[key], hard,
			"后到的短冷却不得缩短 hard_auth 的长冷却")

		runtime.recordFailure(key, KindSoftRateLimit, 30, 0, false, settings)
		pace := runtime.PacedUntil[key]
		runtime.recordFailure(key, KindSoftRateLimit, 3, 0, false, settings)
		assert.GreaterOrEqual(t, runtime.PacedUntil[key], pace,
			"后到的短 Retry-After 不得缩短长减速")
	})
}

// 请求级 400（内容审查/参数不支持）不累计 reject_streak；只有无法识别的
// 配置可疑拒绝才在阈值后写短期冷却。
func TestRequestScopedRejectDoesNotStreak(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-request-scoped-reject", 1, 1)
	runtime := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])
	settings := DefaultCircuitSettings()

	runtime.withLock(func() {
		for i := 0; i < 6; i++ {
			runtime.recordReject(key, KindUpstreamReject, true, true, settings)
		}
		assert.Zero(t, runtime.Circuits[key].RejectStreak)
		assert.Zero(t, runtime.Cooldowns[key])
	})

	runtime.withLock(func() {
		for i := 0; i < 5; i++ {
			runtime.recordReject(key, KindUpstreamReject, true, false, settings)
		}
		assert.Greater(t, runtime.Cooldowns[key], int64(0),
			"配置可疑的连续拒绝达到阈值后应写冷却")
		assert.Zero(t, runtime.Circuits[key].RejectStreak)
	})

	// 请求级拒绝夹在中间必须中断"连续"：4 次配置可疑 + 1 次请求级 + 1 次配置可疑
	// 不得触发冷却，否则计数不是连续的。
	other := testRoute(t, "lane-request-scoped-reset", 1, 1)
	otherRuntime := Default.For(other.Model)
	otherKey := memberKeyOf(&other.Members[0])
	otherRuntime.withLock(func() {
		for i := 0; i < 4; i++ {
			otherRuntime.recordReject(otherKey, KindUpstreamReject, true, false, settings)
		}
		otherRuntime.recordReject(otherKey, KindUpstreamReject, true, true, settings)
		assert.Zero(t, otherRuntime.Circuits[otherKey].RejectStreak,
			"请求级拒绝必须清零既有连续计数")
		otherRuntime.recordReject(otherKey, KindUpstreamReject, true, false, settings)
		assert.Equal(t, 1, otherRuntime.Circuits[otherKey].RejectStreak)
		assert.Zero(t, otherRuntime.Cooldowns[otherKey], "重新从 1 计数，不应触发冷却")
	})
}

func TestPacedMemberIsDeferredButStillSelectable(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-pace-order", 2, 1)
	runtime := Default.For(resolved.Model)
	keyA := memberKeyOf(&resolved.Members[0])
	keyB := memberKeyOf(&resolved.Members[1])

	runtime.withLock(func() {
		runtime.PacedUntil[keyA] = nowMs() + 30_000
	})
	state := NewState(resolved)
	member, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.Equal(t, resolved.Members[1].ChannelId, member.ChannelId,
		"同优先级内应先选未减速成员")

	// 全部减速：仍可选，选最早到期者（A 30s < B 60s），不能把车道判成不可用。
	runtime.withLock(func() {
		runtime.PacedUntil[keyB] = nowMs() + 60_000
	})
	state2 := NewState(resolved)
	member2, _, ok := state2.Next(nil)
	require.True(t, ok, "减速不改变 available")
	assert.Equal(t, resolved.Members[0].ChannelId, member2.ChannelId)
}

func TestClassifyFailedDependencyAndRequestScopedReject(t *testing.T) {
	replay := apiError(http.StatusFailedDependency, types.ErrorCode(""), false)
	assert.Equal(t, KindUpstreamReject, Classify(replay))
	assert.True(t, IsRequestScopedUpstreamReject(replay))

	inspection := types.NewError(
		errors.New("upstream rejected: data_inspection_failed"),
		types.ErrorCodeUpstreamReject,
		types.ErrOptionWithStatusCode(http.StatusBadRequest),
	)
	assert.True(t, IsRequestScopedUpstreamReject(inspection))

	unknown := apiError(http.StatusBadRequest, types.ErrorCodeUpstreamReject, false)
	assert.False(t, IsRequestScopedUpstreamReject(unknown),
		"无法识别的 400 仍按配置可疑处理")
}

// 在飞成功不能绕过探测槽关闭 open；只有携带 ProbeLease 的探测成功才恢复。
func TestInFlightSuccessDoesNotCloseOpenCircuit(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-inflight-success", 1, 1)
	runtime := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])
	settings := DefaultCircuitSettings()

	runtime.withLock(func() {
		for i := 0; i < 4; i++ {
			runtime.recordFailure(key, KindTimeout, 60, 0, true, settings)
		}
		require.Equal(t, CircuitOpen, runtime.Circuits[key].State)
		cooldown := runtime.Cooldowns[key]
		require.Greater(t, cooldown, int64(0))

		runtime.recordSuccess(key, 0, settings, false)
		assert.Equal(t, CircuitOpen, runtime.Circuits[key].State,
			"普通在飞成功不得关闭 open")
		assert.Equal(t, cooldown, runtime.Cooldowns[key],
			"普通在飞成功不得清除冷却")

		runtime.recordSuccess(key, 0, settings, true)
		assert.Equal(t, CircuitClosed, runtime.Circuits[key].State,
			"探测成功才允许恢复")
		assert.Zero(t, runtime.Cooldowns[key])
		assert.Empty(t, runtime.Circuits[key].RecentOutcomes,
			"恢复后清空恢复前的失败样本")
	})
}

func TestHealthReportsOverrideErrorAndHidesExpiredAffinity(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-health-truth", 1, 1)
	resolved.Members[0].OverrideError = &model.LaneConfigError{
		Code: "invalid_value", Message: "bad override", Path: "overrides",
	}
	runtime := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])
	runtime.withLock(func() {
		runtime.HasCurrent = true
		runtime.CurrentMember = key
		runtime.AffinityUntil = nowMs() - 1000
	})

	health := runtime.Health(resolved, DefaultCircuitSettings())
	require.Len(t, health.Members, 1)
	member := health.Members[0]
	assert.False(t, member.Available, "OverrideError 成员不可选")
	require.NotNil(t, member.OverrideError)
	assert.NotNil(t, member.FailsByKind)
	assert.Nil(t, health.Affinity, "过期亲和必须投影为 null")
}

func TestStaleMemberMigrationDeletesOldKeys(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-stale-migration", 1, 1)
	runtime := Default.For(resolved.Model)
	oldKey := MemberKey{ChannelID: 1, UpstreamModel: "model-1", MemberID: 1}
	newKey := MemberKey{ChannelID: 1, UpstreamModel: "model-1", MemberID: 2}

	runtime.withLock(func() {
		runtime.Cooldowns[oldKey] = nowMs() + 60_000
		runtime.Circuits[oldKey] = &Circuit{State: CircuitOpen, OpenUntil: nowMs() + 60_000}
		runtime.migrateStaleMemberKeysLocked(map[MemberKey]bool{newKey: true})
		_, oldCooldown := runtime.Cooldowns[oldKey]
		assert.False(t, oldCooldown, "旧冷却键必须删除")
		assert.Nil(t, runtime.Circuits[oldKey], "旧熔断键必须删除")
		assert.Greater(t, runtime.Cooldowns[newKey], int64(0))
		require.NotNil(t, runtime.Circuits[newKey])

		// 再模拟一次更高版本的迁移：旧键不能复活已清理的状态。
		newerKey := MemberKey{ChannelID: 1, UpstreamModel: "model-1", MemberID: 3}
		runtime.migrateStaleMemberKeysLocked(map[MemberKey]bool{newerKey: true})
		assert.Zero(t, runtime.Cooldowns[newKey], "上一代键也不得复活")
	})
}
