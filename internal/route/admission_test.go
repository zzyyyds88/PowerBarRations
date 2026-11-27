package route

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func waitForQueued(t *testing.T, reg *AdmissionRegistry, key AdmissionKey, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reg.Stats(key).Queued == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("queue did not reach %d (now %d)", want, reg.Stats(key).Queued)
}

func newTestAdmission() *AdmissionRegistry {
	return &AdmissionRegistry{members: map[AdmissionKey]*admissionMember{}}
}

func withAdmissionClock(t *testing.T) *time.Time {
	t.Helper()
	original := admissionNow
	clock := time.Unix(1_700_000_000, 0)
	admissionNow = func() time.Time { return clock }
	t.Cleanup(func() { admissionNow = original })
	return &clock
}

func TestAdmissionEpochPreventsOldTicketFeedback(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := AdmissionKey{ChannelID: 9, Model: "model-epoch"}
	cfg := RateLimitConfig{Mode: AdmissionModeAdaptive, QueueLimit: 2}

	oldTicket, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	reg.Invalidate(key)
	newTicket, err := reg.Acquire(key, cfg)
	require.NoError(t, err)

	reg.mu.Lock()
	before := reg.members[key].limit
	reg.mu.Unlock()
	oldTicket.Release(AdmissionFailure, KindSoftRateLimit, 0)
	reg.mu.Lock()
	after := reg.members[key].limit
	reg.mu.Unlock()
	assert.Equal(t, before, after, "旧代次 ticket 不得污染新代次 AIMD 状态")
	newTicket.Release(AdmissionSuccess, "", 0)
}

func TestAdmissionKeyAndOutcome(t *testing.T) {
	key := AdmissionKeyOf(&model.RouteMember{ChannelId: 7, Model: " model-a "})
	assert.Equal(t, AdmissionKey{ChannelID: 7, Model: "model-a"}, key)
	assert.Equal(t, "7:model-a", key.String())
	assert.Equal(t, AdmissionKey{}, AdmissionKeyOf(nil))

	assert.Equal(t, AdmissionCanceled, AdmissionOutcomeOf(context.Canceled))
	assert.Equal(t, AdmissionNeutral, AdmissionOutcomeOf(ErrAdmissionWaitTimeout))
	assert.Equal(t, AdmissionNeutral, AdmissionOutcomeOf(ErrAdmissionInvalidated))
	assert.Equal(t, AdmissionSuccess, AdmissionOutcomeOf(nil))
}

func TestAdmissionConcurrencySlots(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := AdmissionKey{ChannelID: 1, Model: "model-a"}
	cfg := RateLimitConfig{Mode: AdmissionModeFixed, MaxConcurrency: 2, QueueLimit: 2}

	first, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	second, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	_, err = reg.Acquire(key, cfg)
	assert.ErrorIs(t, err, ErrAdmissionNotReady, "第三个请求必须让位")

	first.Release(AdmissionSuccess, "", 0)
	third, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	second.Release(AdmissionSuccess, "", 0)
	third.Release(AdmissionSuccess, "", 0)
	assert.Equal(t, 0, reg.Stats(key, cfg).Inflight)
}

func TestAdmissionRPMWindowSlides(t *testing.T) {
	clock := withAdmissionClock(t)
	reg := newTestAdmission()
	key := AdmissionKey{ChannelID: 2, Model: "model-b"}
	cfg := RateLimitConfig{Mode: AdmissionModeFixed, MaxRPM: 2, QueueLimit: 2}

	t1, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	t2, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	_, err = reg.Acquire(key, cfg)
	assert.ErrorIs(t, err, ErrAdmissionNotReady)
	assert.Equal(t, 2, reg.Stats(key, cfg).RPMUsed)

	// 窗口滑出后重新就绪（不依赖释放动作）。
	*clock = clock.Add(61 * time.Second)
	t3, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	t1.Release(AdmissionSuccess, "", 0)
	t2.Release(AdmissionSuccess, "", 0)
	t3.Release(AdmissionSuccess, "", 0)
}

func TestAdmissionQueueFIFO(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := AdmissionKey{ChannelID: 3, Model: "model-c"}
	cfg := RateLimitConfig{Mode: AdmissionModeFixed, MaxConcurrency: 1, QueueLimit: 4, QueueWaitSeconds: 5}

	held, err := reg.Acquire(key, cfg)
	require.NoError(t, err)

	waiter1 := make(chan *AdmissionTicket, 1)
	waiter2 := make(chan *AdmissionTicket, 1)
	go func() {
		ticket, _, waitErr := reg.Wait(context.Background(), key, "lane", cfg)
		if waitErr == nil {
			waiter1 <- ticket
		}
	}()
	waitForQueued(t, reg, key, 1)
	go func() {
		ticket, _, waitErr := reg.Wait(context.Background(), key, "lane", cfg)
		if waitErr == nil {
			waiter2 <- ticket
		}
	}()
	waitForQueued(t, reg, key, 2)

	held.Release(AdmissionSuccess, "", 0)
	var first *AdmissionTicket
	select {
	case first = <-waiter1:
	case <-time.After(2 * time.Second):
		t.Fatal("first waiter should be woken first")
	}
	select {
	case <-waiter2:
		t.Fatal("FIFO：第二个等待者不能越过第一个")
	case <-time.After(100 * time.Millisecond):
	}
	first.Release(AdmissionSuccess, "", 0)
	select {
	case second := <-waiter2:
		second.Release(AdmissionSuccess, "", 0)
	case <-time.After(2 * time.Second):
		t.Fatal("second waiter should be admitted after release")
	}
}

func TestAdmissionQueueFull(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := AdmissionKey{ChannelID: 4, Model: "model-d"}
	cfg := RateLimitConfig{Mode: AdmissionModeFixed, MaxConcurrency: 1, QueueLimit: 1, QueueWaitSeconds: 5}
	held, err := reg.Acquire(key, cfg)
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = reg.Wait(context.Background(), key, "lane", cfg)
	}()
	waitForQueued(t, reg, key, 1)

	_, wait, err := reg.Wait(context.Background(), key, "lane", cfg)
	assert.ErrorIs(t, err, ErrAdmissionQueueFull)
	assert.GreaterOrEqual(t, wait, time.Second)

	held.Release(AdmissionSuccess, "", 0)
	<-done
}

func TestAdmissionAdaptiveAIMD(t *testing.T) {
	clock := withAdmissionClock(t)
	reg := newTestAdmission()
	key := AdmissionKey{ChannelID: 5, Model: "model-e"}
	cfg := RateLimitConfig{Mode: AdmissionModeAdaptive}
	tickets := make([]*AdmissionTicket, 0, 8)
	for i := 0; i < 4; i++ {
		ticket, err := reg.Acquire(key, cfg)
		require.NoError(t, err)
		tickets = append(tickets, ticket)
	}
	stats := reg.Stats(key, cfg)
	require.True(t, stats.Adaptive)
	assert.InDelta(t, 4, stats.AdaptiveLimit, 1e-9, "初始上限 = max(2, ceiling/8)")
	_, err := reg.Acquire(key, cfg)
	assert.ErrorIs(t, err, ErrAdmissionNotReady)

	// 打满时成功一次 → 加性增长。
	tickets[3].Release(AdmissionSuccess, "", 100)
	assert.Greater(t, reg.Stats(key, cfg).AdaptiveLimit, 4.0)

	// 429 → 减半并暂停 1 秒。
	limitBefore := reg.Stats(key, cfg).AdaptiveLimit
	tickets[2].Release(AdmissionFailure, KindSoftRateLimit, 100)
	limitAfter := reg.Stats(key, cfg).AdaptiveLimit
	assert.LessOrEqual(t, limitAfter, limitBefore/2+0.001)
	assert.False(t, reg.Ready(key, cfg), "429 后自适应成员暂停 1 秒")

	for i := 0; i < 2; i++ {
		tickets[i].Release(AdmissionSuccess, "", 0)
	}
	assert.False(t, reg.Ready(key, cfg), "暂停未到期仍不可就绪")
	*clock = clock.Add(2 * time.Second)
	assert.True(t, reg.Ready(key, cfg), "暂停到期后恢复就绪")
}

func TestAdmissionModeSwitchClearsPauseAndIgnoresOldAdaptiveTicket(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := AdmissionKey{ChannelID: 11, Model: "model-k"}
	adaptive := RateLimitConfig{Mode: AdmissionModeAdaptive}
	oldTicket, err := reg.Acquire(key, adaptive)
	require.NoError(t, err)
	limitBefore := reg.Stats(key, adaptive).AdaptiveLimit

	fixed := RateLimitConfig{Mode: AdmissionModeFixed, MaxConcurrency: 2}
	assert.True(t, reg.Ready(key, fixed), "切到 fixed 必须清除 adaptive 暂停")
	oldTicket.Release(AdmissionFailure, KindSoftRateLimit, 100)
	assert.Equal(t, limitBefore, reg.Stats(key, adaptive).AdaptiveLimit, "旧 adaptive ticket 不得写新 fixed 模式")
	assert.True(t, reg.Ready(key, fixed), "旧 adaptive ticket 不得给 fixed 模式写暂停")

	adaptiveTicket, err := reg.Acquire(key, adaptive)
	require.NoError(t, err)
	adaptiveTicket.Release(AdmissionFailure, KindSoftRateLimit, 100)
	assert.False(t, reg.Ready(key, adaptive))
	assert.True(t, reg.Ready(key, RateLimitConfig{Mode: AdmissionModeDisabled}))
	assert.True(t, reg.Ready(key, adaptive), "adaptive→disabled 必须清除旧暂停")
}

func TestAdmissionNeutralAndCanceledDoNotChangeAIMD(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := AdmissionKey{ChannelID: 6, Model: "model-f"}
	cfg := RateLimitConfig{Mode: AdmissionModeAdaptive}
	ticket, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	before := reg.Stats(key, cfg).AdaptiveLimit
	ticket.Release(AdmissionNeutral, KindTimeout, 5000)
	assert.Equal(t, before, reg.Stats(key, cfg).AdaptiveLimit)

	ticket, err = reg.Acquire(key, cfg)
	require.NoError(t, err)
	ticket.Release(AdmissionCanceled, KindSoftRateLimit, 5000)
	assert.Equal(t, before, reg.Stats(key, cfg).AdaptiveLimit)
	assert.True(t, reg.Ready(key, cfg), "canceled 不得写入 429 暂停")
}

func TestAdmissionStatsUsesCurrentConfigMode(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := AdmissionKey{ChannelID: 7, Model: "model-g"}
	adaptive := RateLimitConfig{Mode: AdmissionModeAdaptive}
	_, err := reg.Acquire(key, adaptive)
	require.NoError(t, err)
	assert.True(t, reg.Stats(key, adaptive).Adaptive)

	fixed := RateLimitConfig{Mode: AdmissionModeFixed, MaxConcurrency: 3}
	stats := reg.Stats(key, fixed)
	assert.False(t, stats.Adaptive, "Stats 必须按当前 cfg 报告，不信任历史 adaptive")
	assert.Equal(t, AdmissionModeFixed, stats.Mode)

	disabled := RateLimitConfig{Mode: AdmissionModeDisabled}
	assert.True(t, reg.Ready(key, disabled))
	assert.Equal(t, 1, reg.Stats(key, disabled).Inflight, "disabled 模式仍应报告切换前真实在飞数")
	ticket, err := reg.Acquire(key, disabled)
	require.NoError(t, err)
	assert.Nil(t, ticket)
}

func TestAdmissionWaiterCancellation(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := AdmissionKey{ChannelID: 8, Model: "model-h"}
	cfg := RateLimitConfig{Mode: AdmissionModeFixed, MaxConcurrency: 1, QueueLimit: 4, QueueWaitSeconds: 5}
	held, err := reg.Acquire(key, cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, waitErr := reg.Wait(ctx, key, "lane", cfg)
		done <- waitErr
	}()
	waitForQueued(t, reg, key, 1)
	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("canceled waiter did not exit")
	}
	assert.Equal(t, 0, reg.Stats(key, cfg).Queued)
	held.Release(AdmissionSuccess, "", 0)
}

func TestAdmissionWaiterDeadline(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := AdmissionKey{ChannelID: 9, Model: "model-i"}
	cfg := RateLimitConfig{Mode: AdmissionModeFixed, MaxConcurrency: 1, QueueLimit: 4, QueueWaitSeconds: 5}
	held, err := reg.Acquire(key, cfg)
	require.NoError(t, err)

	deadline := time.Now().Add(30 * time.Millisecond)
	_, _, err = reg.Wait(context.Background(), key, "lane", cfg, deadline)
	assert.ErrorIs(t, err, ErrAdmissionWaitTimeout)
	assert.Equal(t, 0, reg.Stats(key, cfg).Queued)
	held.Release(AdmissionSuccess, "", 0)
}

func TestAdmissionInvalidateWakesWaitersAndDeleteIdle(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := AdmissionKey{ChannelID: 10, Model: "model-j"}
	cfg := RateLimitConfig{Mode: AdmissionModeFixed, MaxConcurrency: 1, QueueLimit: 4, QueueWaitSeconds: 5}
	held, err := reg.Acquire(key, cfg)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, _, waitErr := reg.Wait(context.Background(), key, "lane", cfg)
		done <- waitErr
	}()
	waitForQueued(t, reg, key, 1)
	reg.Invalidate(key)
	select {
	case err := <-done:
		assert.ErrorIs(t, err, ErrAdmissionInvalidated)
	case <-time.After(2 * time.Second):
		t.Fatal("invalidated waiter did not exit")
	}
	assert.Equal(t, 1, reg.Stats(key, cfg).Inflight, "仍有在飞请求时不得删除计数")
	reg.DeleteIdle(key)
	assert.Equal(t, 1, reg.Stats(key, cfg).Inflight)
	held.Release(AdmissionSuccess, "", 0)
	reg.mu.Lock()
	assert.Nil(t, reg.members[key], "retired 条目最后一个 ticket 释放后应删除")
	reg.mu.Unlock()
	reg.DeleteIdle(key)
	assert.Equal(t, AdmissionStats{Mode: AdmissionModeFixed}, reg.Stats(key, cfg))
}

func TestLocalThrottleErrorShape(t *testing.T) {
	err := LocalThrottleError("lane-x", 2500*time.Millisecond)
	require.NotNil(t, err)
	assert.Equal(t, 429, err.StatusCode)
	assert.Equal(t, CodeLocalThrottle, string(err.GetErrorCode()))
	assert.Equal(t, 3, err.RetryAfterSeconds, "Retry-After 向上取整，至少 1 秒")
	assert.True(t, types.IsSkipRetryError(err), "本地准入拒绝不得触发换人重试")
	openAI := err.ToOpenAIError()
	assert.Equal(t, "rate_limit_error", openAI.Type)
	assert.Equal(t, CodeLocalThrottle, openAI.Code)
	assert.True(t, IsLocalThrottle(err))
	assert.False(t, IsNoAvailable(err), "429 不是 503")
}

func TestStateAdmissionGivesWayAndCommits(t *testing.T) {
	withFakeClock(t)
	state := NewState(testRoute(t, "lane-admission-select", 2, 1))
	// 只有低优先级成员就绪：选路必须直接顶上，而不是等 P1。
	state.SetAdmissionReady(func(member *model.RouteMember) bool {
		return member.Channel == "channel-b"
	})
	member, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", member.Channel, "就绪的低优先级成员应先服务")

	// 全部未就绪：Next 返回 false，但 ThrottledMember 给出可排队候选；
	// 取到槽位后 CommitThrottled 提交该成员（不重复选路）。
	blocked := NewState(testRoute(t, "lane-admission-select", 2, 1))
	blocked.SetAdmissionReady(func(*model.RouteMember) bool { return false })
	_, _, ok = blocked.Next(nil)
	require.False(t, ok)
	candidate := blocked.ThrottledMember()
	require.NotNil(t, candidate)
	assert.Equal(t, "channel-a", candidate.Channel)

	// Wait 取得槽位后 inflight 已计入本次请求，就绪回调此刻必然返回 false；
	// 槽位归属由 ticket 保证，CommitThrottled 不得再按 admissionReady 复核。
	committed, ok := blocked.CommitThrottled(120)
	require.True(t, ok)
	assert.Equal(t, "channel-a", committed.Channel)
	assert.Equal(t, int64(120), blocked.SuccessAttempt("channel-a/model").WaitMs,
		"成功尝试必须带实际排队时长")
	for _, attempt := range blocked.LogAttempts() {
		assert.NotEqual(t, memberLabel(candidate), attempt.Member,
			"排队成功的成员不得再留 throttled 跳过记录：它实际被使用了")
	}

	// 排队期间该成员被同车道其它请求打进冷却：即使已取得准入槽位也不得提交。
	cooling := NewState(testRoute(t, "lane-admission-select", 2, 1))
	cooling.SetAdmissionReady(func(*model.RouteMember) bool { return false })
	_, _, ok = cooling.Next(nil)
	require.False(t, ok)
	coolingCandidate := cooling.ThrottledMember()
	require.NotNil(t, coolingCandidate)
	coolingKey := memberKeyOf(coolingCandidate)
	cooling.Runtime.withLock(func() { cooling.Runtime.Cooldowns[coolingKey] = nowMs() + 60_000 })
	_, ok = cooling.CommitThrottled(120)
	assert.False(t, ok, "排队期间进入冷却的成员不得提交")
}

// 准入等待失败：throttled attempt 必须带回实际 wait_ms（api-spec §4.4），
// 且不能覆盖其他成员的跳过记录。
func TestExhaustThrottledRecordsWait(t *testing.T) {
	state := NewState(testRoute(t, "lane-admission-exhaust-wait", 2, 1))
	state.SetAdmissionReady(func(*model.RouteMember) bool { return false })
	_, _, ok := state.Next(nil)
	require.False(t, ok)
	require.NotNil(t, state.ThrottledMember())

	state.ExhaustThrottled(250)

	attempts := state.LogAttempts()
	throttled := 0
	for _, attempt := range attempts {
		if attempt.Status != "throttled" {
			continue
		}
		throttled++
		if attempt.WaitMs == 250 {
			return
		}
	}
	t.Fatalf("throttled attempt 未记录 wait_ms=250: %+v", attempts)
}

// 先被 throttled 跳过的成员在后续轮次真正被选中/执行时，跳过记录必须消失
// （throttled 只表示"未被使用"）；不能只在 CommitThrottled 一个入口清理。
func TestThrottledSkipRemovedWhenMemberSelectedLater(t *testing.T) {
	state := NewState(testRoute(t, "lane-admission-late-select", 2, 1))
	aReady := false
	state.SetAdmissionReady(func(member *model.RouteMember) bool {
		return member.Channel == "channel-b" || aReady
	})

	member, _, ok := state.Next(nil)
	require.True(t, ok)
	require.Equal(t, "channel-b", member.Channel)

	// 下一轮 A 已就绪：B 失败后应直接选中 A，并清掉早先的 throttled(A)。
	aReady = true
	_, _, ok = state.Next(types.NewOpenAIError(errors.New("boom"), types.ErrorCode(""), 502))
	require.True(t, ok)

	for _, attempt := range state.LogAttempts() {
		assert.NotEqual(t, "throttled", attempt.Status,
			"实际被执行的成员不应保留 throttled 跳过记录: %+v", state.LogAttempts())
	}
}

// 排队成功的请求随后在上游失败时，failed attempt 也必须保留排队时长；
// 否则 wait_ms 只在"排队失败"时出现，成功拿到槽位的等待完全丢失。
func TestFailedAttemptKeepsQueuedWait(t *testing.T) {
	state := NewState(testRoute(t, "lane-admission-wait", 1, 1))
	state.SetAdmissionReady(func(*model.RouteMember) bool { return false })
	_, _, ok := state.Next(nil)
	require.False(t, ok)
	require.NotNil(t, state.ThrottledMember())

	_, ok = state.CommitThrottled(250)
	require.True(t, ok)

	state.Next(types.NewOpenAIError(errors.New("boom"), types.ErrorCode(""), 500))

	attempts := state.LogAttempts()
	require.NotEmpty(t, attempts)
	failed := attempts[len(attempts)-1]
	assert.Equal(t, "failed", failed.Status)
	assert.Equal(t, int64(250), failed.WaitMs)
}
