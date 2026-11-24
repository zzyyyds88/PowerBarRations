package route

import (
	"context"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func waitForQueued(t *testing.T, reg *AdmissionRegistry, key string, want int) {
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
	return &AdmissionRegistry{members: map[string]*admissionMember{}}
}

func withAdmissionClock(t *testing.T) *time.Time {
	t.Helper()
	original := admissionNow
	clock := time.Unix(1_700_000_000, 0)
	admissionNow = func() time.Time { return clock }
	t.Cleanup(func() { admissionNow = original })
	return &clock
}

func TestAdmissionConcurrencySlots(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := "1:model-a"
	cfg := RateLimitConfig{MaxConcurrency: 2, QueueLimit: 2}

	first, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	second, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	_, err = reg.Acquire(key, cfg)
	assert.ErrorIs(t, err, ErrAdmissionNotReady, "第三个请求必须让位")

	first.Release("", 0)
	third, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	second.Release("", 0)
	third.Release("", 0)
	assert.Equal(t, 0, reg.Stats(key).Inflight)
}

func TestAdmissionRPMWindowSlides(t *testing.T) {
	clock := withAdmissionClock(t)
	reg := newTestAdmission()
	key := "2:model-b"
	cfg := RateLimitConfig{MaxRPM: 2, QueueLimit: 2}

	t1, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	t2, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	_, err = reg.Acquire(key, cfg)
	assert.ErrorIs(t, err, ErrAdmissionNotReady)
	assert.Equal(t, 2, reg.Stats(key).RPMUsed)

	// 窗口滑出后重新就绪（不依赖释放动作）。
	*clock = clock.Add(61 * time.Second)
	t3, err := reg.Acquire(key, cfg)
	require.NoError(t, err)
	t1.Release("", 0)
	t2.Release("", 0)
	t3.Release("", 0)
}

func TestAdmissionQueueFIFO(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := "3:model-c"
	cfg := RateLimitConfig{MaxConcurrency: 1, QueueLimit: 4, QueueWaitSeconds: 5}

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

	held.Release("", 0)
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
	first.Release("", 0)
	select {
	case second := <-waiter2:
		second.Release("", 0)
	case <-time.After(2 * time.Second):
		t.Fatal("second waiter should be admitted after release")
	}
}

func TestAdmissionQueueFull(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := "4:model-d"
	cfg := RateLimitConfig{MaxConcurrency: 1, QueueLimit: 1, QueueWaitSeconds: 5}
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

	held.Release("", 0)
	<-done
}

func TestAdmissionAdaptiveAIMD(t *testing.T) {
	withAdmissionClock(t)
	reg := newTestAdmission()
	key := "5:model-e"
	cfg := RateLimitConfig{MaxConcurrency: -1}
	tickets := make([]*AdmissionTicket, 0, 8)
	for i := 0; i < 4; i++ {
		ticket, err := reg.Acquire(key, cfg)
		require.NoError(t, err)
		tickets = append(tickets, ticket)
	}
	stats := reg.Stats(key)
	require.True(t, stats.Adaptive)
	assert.InDelta(t, 4, stats.AdaptiveLimit, 1e-9, "初始上限 = max(2, ceiling/8)")
	_, err := reg.Acquire(key, cfg)
	assert.ErrorIs(t, err, ErrAdmissionNotReady)

	// 打满时成功一次 → 加性增长。
	tickets[3].Release("", 100)
	assert.Greater(t, reg.Stats(key).AdaptiveLimit, 4.0)

	// 429 → 减半并暂停 1 秒。
	limitBefore := reg.Stats(key).AdaptiveLimit
	tickets[2].Release(KindSoftRateLimit, 100)
	limitAfter := reg.Stats(key).AdaptiveLimit
	assert.LessOrEqual(t, limitAfter, limitBefore/2+0.001)
	assert.False(t, reg.Ready(key, cfg), "429 后自适应成员暂停 1 秒")

	// 释放其余在飞请求，只验证"暂停到期"这一条。
	for i := 0; i < 2; i++ {
		tickets[i].Release("", 0)
	}
	assert.False(t, reg.Ready(key, cfg), "暂停未到期仍不可就绪")
	clock := admissionNow()
	admissionNow = func() time.Time { return clock.Add(2 * time.Second) }
	assert.True(t, reg.Ready(key, cfg), "暂停到期后恢复就绪")
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
	state := NewState(testRoute("lane-admission-select", 2, 1))
	// 只有低优先级成员就绪：选路必须直接顶上，而不是等 P1。
	state.SetAdmissionReady(func(member *model.RouteMember) bool {
		return member.Channel == "channel-b"
	})
	member, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", member.Channel, "就绪的低优先级成员应先服务")

	// 全部未就绪：Next 返回 false，但 ThrottledMember 给出可排队候选；
	// 取到槽位后 CommitThrottled 提交该成员（不重复选路）。
	blocked := NewState(testRoute("lane-admission-select", 2, 1))
	blocked.SetAdmissionReady(func(*model.RouteMember) bool { return false })
	_, _, ok = blocked.Next(nil)
	require.False(t, ok)
	candidate := blocked.ThrottledMember()
	require.NotNil(t, candidate)
	assert.Equal(t, "channel-a", candidate.Channel)

	// Wait 取得槽位后 inflight 已计入本次请求，就绪回调此刻必然返回 false；
	// 槽位归属由 ticket 保证，CommitThrottled 不得再按 admissionReady 复核，
	// 否则排队成功的请求会把自己刚占的槽判成已满并回落为本地 429。
	committed, ok := blocked.CommitThrottled()
	require.True(t, ok)
	assert.Equal(t, "channel-a", committed.Channel)

	// 排队期间该成员被同车道其它请求打进冷却：即使已取得准入槽位也不得提交，
	// 调用方会释放槽位并改选其它成员。
	cooling := NewState(testRoute("lane-admission-select", 2, 1))
	cooling.SetAdmissionReady(func(*model.RouteMember) bool { return false })
	_, _, ok = cooling.Next(nil)
	require.False(t, ok)
	coolingCandidate := cooling.ThrottledMember()
	require.NotNil(t, coolingCandidate)
	coolingKey := memberKeyOf(coolingCandidate)
	cooling.Runtime.withLock(func() { cooling.Runtime.Cooldowns[coolingKey] = nowMs() + 60_000 })
	_, ok = cooling.CommitThrottled()
	assert.False(t, ok, "排队期间进入冷却的成员不得提交")
}
