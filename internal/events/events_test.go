package events

import (
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupportedEventTypesIsCanonicalAndDetached(t *testing.T) {
	types := SupportedEventTypes()
	sort.Strings(types)
	expected := []string{
		EventChannelDeleted,
		EventChannelDisabled,
		EventChannelEnabled,
		EventCircuitClosed,
		EventCircuitHalfOpen,
		EventCircuitOpen,
		EventCooldown,
		EventReset,
		EventSkip,
		EventUpstreamBurst,
	}
	sort.Strings(expected)
	assert.Equal(t, expected, types)

	types[0] = "mutated"
	assert.True(t, IsSupportedType(expected[0]))
	assert.False(t, IsSupportedType("mutated"))
	assert.True(t, IsSupportedType(EventChannelDisabled))
	assert.True(t, IsSupportedType(EventChannelDeleted))
	assert.True(t, IsSupportedType(EventUpstreamBurst))
	assert.False(t, IsSupportedType("affinity_start"))
}

func TestSetSubscriberAndPublish(t *testing.T) {
	SetSubscriber(nil)
	t.Cleanup(func() { SetSubscriber(nil) })

	var got []Event
	SetSubscriber(func(event Event) { got = append(got, event) })
	Publish(Event{Ts: 42, Type: EventChannelDisabled, Member: "7"})

	require.Len(t, got, 1)
	assert.Equal(t, Event{Ts: 42, Type: EventChannelDisabled, Member: "7"}, got[0])

	SetSubscriber(nil)
	Publish(Event{Type: EventChannelEnabled})
	assert.Len(t, got, 1)
}

func TestSetSubscriberReplacesPreviousSubscriber(t *testing.T) {
	SetSubscriber(nil)
	t.Cleanup(func() { SetSubscriber(nil) })

	first, second := 0, 0
	SetSubscriber(func(Event) { first++ })
	SetSubscriber(func(Event) { second++ })
	Publish(Event{Type: EventUpstreamBurst})

	assert.Zero(t, first)
	assert.Equal(t, 1, second)
}

func TestPublishAllowsSubscriberToUnsubscribe(t *testing.T) {
	SetSubscriber(nil)
	t.Cleanup(func() { SetSubscriber(nil) })

	called := 0
	SetSubscriber(func(Event) {
		called++
		SetSubscriber(nil)
	})
	Publish(Event{Type: EventChannelDeleted})
	Publish(Event{Type: EventChannelDeleted})

	assert.Equal(t, 1, called)
}

func TestConcurrentPublishAndSetSubscriber(t *testing.T) {
	SetSubscriber(nil)
	t.Cleanup(func() { SetSubscriber(nil) })

	var mu sync.Mutex
	count := 0
	SetSubscriber(func(Event) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				Publish(Event{Type: EventChannelEnabled})
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 800, count)
}
