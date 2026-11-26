package route

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func probeTestState(t *testing.T) (*State, *ProbeLease) {
	t.Helper()
	runtime := newRuntime()
	key := MemberKey{ChannelID: 1, UpstreamModel: "model", MemberID: 1}
	lease, ok := runtime.AcquireProbe(key, time.Minute)
	require.True(t, ok)
	return &State{Runtime: runtime, probe: lease}, lease
}

// 同成员重试等待期间客户端取消，必须归还本请求持有的唯一探测槽。
func TestWaitRetryCancellationReleasesProbe(t *testing.T) {
	state, _ := probeTestState(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.False(t, state.WaitRetry(ctx, time.Second))
	assert.Nil(t, state.Runtime.probe)
	assert.Nil(t, state.probe)
}

func TestWaitRetryCompletedDelayKeepsProbe(t *testing.T) {
	state, lease := probeTestState(t)
	assert.True(t, state.WaitRetry(context.Background(), time.Millisecond))
	assert.Same(t, lease, state.Runtime.probe)
	assert.Equal(t, lease.key, state.Runtime.probe.key)
}
