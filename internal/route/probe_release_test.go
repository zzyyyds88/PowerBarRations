package route

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// 同成员重试等待期间客户端取消，必须归还本请求持有的唯一探测槽；
// 否则该车道后续所有半开探测都会被一个已取消的请求永久阻塞。
func TestWaitRetryCancellationReleasesProbe(t *testing.T) {
	state := &State{Runtime: &Runtime{}}
	state.claimedProbe = "1:model#1"
	state.Runtime.HasProbe = true
	state.Runtime.ProbeMember = "1:model#1"

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.False(t, state.WaitRetry(ctx, time.Second))
	assert.False(t, state.Runtime.HasProbe)
	assert.Empty(t, state.Runtime.ProbeMember)
	assert.Empty(t, state.claimedProbe)
}

func TestWaitRetryCompletedDelayKeepsProbe(t *testing.T) {
	state := &State{Runtime: &Runtime{}}
	state.claimedProbe = "1:model#1"
	state.Runtime.HasProbe = true
	state.Runtime.ProbeMember = "1:model#1"

	assert.True(t, state.WaitRetry(context.Background(), time.Millisecond))
	assert.True(t, state.Runtime.HasProbe)
	assert.Equal(t, "1:model#1", state.Runtime.ProbeMember)
}
