package common

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayExecutorRejectsWhenBoundedQueueIsFull(t *testing.T) {
	executor := newRelayExecutor(1, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	secondRan := make(chan struct{})

	require.True(t, executor.submit(context.Background(), func() {
		close(started)
		<-release
	}))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first task did not start")
	}

	require.True(t, executor.submit(context.Background(), func() {
		close(secondRan)
	}))
	require.False(t, executor.submit(context.Background(), func() {}))
	assert.Equal(t, uint64(1), executor.dropped.Load())

	close(release)
	select {
	case <-secondRan:
	case <-time.After(time.Second):
		t.Fatal("queued task did not run after worker drained")
	}
}

func TestRelayExecutorCancelledSubmissionDoesNotCountAsDrop(t *testing.T) {
	executor := newRelayExecutor(1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.False(t, executor.submit(ctx, func() {}))
	assert.Zero(t, executor.dropped.Load())
}

func TestRelayExecutorRecoversPanicAndSignalsStop(t *testing.T) {
	executor := newRelayExecutor(1, 1)
	stopChan := make(chan bool, 1)
	ctx := context.WithValue(context.Background(), "stop_chan", stopChan)
	var ran atomic.Bool

	require.True(t, executor.submit(ctx, func() { panic("boom") }))
	select {
	case <-stopChan:
	case <-time.After(time.Second):
		t.Fatal("panic did not signal stream stop")
	}

	require.True(t, executor.submit(context.Background(), func() { ran.Store(true) }))
	require.Eventually(t, ran.Load, time.Second, time.Millisecond)
}
