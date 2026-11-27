package route

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 事件订阅钩子（design-v1 §16.10）：appendEvent 是运行态事件唯一出口，
// 订阅者必须收到事件且事件携带车道键（lane 字段供 webhook 投递使用）。
func TestAppendEventNotifiesSubscriberWithLane(t *testing.T) {
	withFakeClock(t)

	ch := make(chan Event, 16)
	SetEventSubscriber(func(ev Event) { ch <- ev })
	t.Cleanup(func() { SetEventSubscriber(nil) })

	runtime := freshRuntime(t, "lane-subscribe-test")
	runtime.withLock(func() {
		// 硬失败权重 1.0 < 阈值 2：只触发 cooldown 事件，不打开熔断。
		runtime.recordFailure(MemberKey{ChannelID: 7, UpstreamModel: "model-x"}, KindHardAuth, 60, 0, true, DefaultCircuitSettings())
	})
	last := waitRouteEvent(t, ch, func(ev Event) bool {
		return ev.Type == EventCooldown && ev.Lane == "lane-subscribe-test"
	})
	assert.Equal(t, "7:model-x", last.Member)
	assert.Contains(t, last.Detail, "cooldown_until=")
	assert.NotZero(t, last.Ts)

	// 事件同时留在车道的运行态事件表里（带 lane 字段）。
	runtime.withLock(func() {
		require.NotEmpty(t, runtime.Events)
		assert.Equal(t, "lane-subscribe-test", runtime.Events[len(runtime.Events)-1].Lane)
	})
}

// 注销后不再转发（SetEventSubscriber(nil)）。
func TestEventSubscriberCanBeUnset(t *testing.T) {
	withFakeClock(t)

	var called atomic.Bool
	SetEventSubscriber(func(Event) { called.Store(true) })
	SetEventSubscriber(nil)

	runtime := freshRuntime(t, "lane-unset-test")
	runtime.withLock(func() {
		runtime.recordFailure(MemberKey{ChannelID: 7, UpstreamModel: "model-x"}, KindHardAuth, 60, 0, true, DefaultCircuitSettings())
	})
	time.Sleep(100 * time.Millisecond)
	assert.False(t, called.Load())
}

// reset 事件（手动复通 circuits/reset）经订阅钩子转发，为车道级（member 空）。
func TestResetNotifiesSubscriber(t *testing.T) {
	withFakeClock(t)

	ch := make(chan Event, 16)
	SetEventSubscriber(func(ev Event) { ch <- ev })
	t.Cleanup(func() { SetEventSubscriber(nil) })

	runtime := freshRuntime(t, "lane-reset-test")
	runtime.withLock(func() {
		runtime.recordFailure(MemberKey{ChannelID: 7, UpstreamModel: "model-x"}, KindHardAuth, 60, 0, true, DefaultCircuitSettings())
	})
	waitRouteEvent(t, ch, func(ev Event) bool {
		return ev.Type == EventCooldown && ev.Lane == "lane-reset-test"
	})
	runtime.Reset()

	last := waitRouteEvent(t, ch, func(ev Event) bool {
		return ev.Type == EventReset && ev.Lane == "lane-reset-test"
	})
	assert.Equal(t, "", last.Member, "reset 是车道级事件，member 为空")
	assert.NotZero(t, last.Ts)
}

func waitRouteEvent(t *testing.T, ch <-chan Event, match func(Event) bool) Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if match(ev) {
				return ev
			}
		case <-deadline:
			t.Fatal("timed out waiting for route event")
			return Event{}
		}
	}
}
