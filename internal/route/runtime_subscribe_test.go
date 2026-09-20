package route

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 事件订阅钩子（design-v1 §16.10）：appendEvent 是运行态事件唯一出口，
// 订阅者必须收到事件且事件携带车道键（lane 字段供 webhook 投递使用）。
func TestAppendEventNotifiesSubscriberWithLane(t *testing.T) {
	withFakeClock(t)

	var events []Event
	SetEventSubscriber(func(ev Event) { events = append(events, ev) })
	t.Cleanup(func() { SetEventSubscriber(nil) })

	runtime := Default.For("lane-subscribe-test")
	runtime.withLock(func() {
		// 硬失败权重 1.0 < 阈值 2：只触发 cooldown 事件，不打开熔断。
		runtime.recordFailure("7:model-x", KindHardAuth, 60, true, DefaultCircuitSettings())
	})
	require.NotEmpty(t, events)
	last := events[len(events)-1]
	assert.Equal(t, EventCooldown, last.Type)
	assert.Equal(t, "lane-subscribe-test", last.Lane, "事件必须携带车道键")
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

	called := false
	SetEventSubscriber(func(Event) { called = true })
	SetEventSubscriber(nil)

	runtime := Default.For("lane-unset-test")
	runtime.withLock(func() {
		runtime.recordFailure("7:model-x", KindHardAuth, 60, true, DefaultCircuitSettings())
	})
	assert.False(t, called)
}

// reset 事件（手动复通 circuits/reset）经订阅钩子转发，为车道级（member 空）。
func TestResetNotifiesSubscriber(t *testing.T) {
	withFakeClock(t)

	var events []Event
	SetEventSubscriber(func(ev Event) { events = append(events, ev) })
	t.Cleanup(func() { SetEventSubscriber(nil) })

	runtime := Default.For("lane-reset-test")
	runtime.withLock(func() {
		runtime.recordFailure("7:model-x", KindHardAuth, 60, true, DefaultCircuitSettings())
	})
	require.NotEmpty(t, events)
	runtime.Reset()

	require.NotEmpty(t, events)
	last := events[len(events)-1]
	assert.Equal(t, EventReset, last.Type)
	assert.Equal(t, "lane-reset-test", last.Lane, "reset 事件必须携带车道键")
	assert.Equal(t, "", last.Member, "reset 是车道级事件，member 为空")
	assert.NotZero(t, last.Ts)
}
