package perfmetrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
)

func TestSnapshotRelaySampleCopiesOnlySafeValues(t *testing.T) {
	start := time.Now().Add(-2 * time.Second)
	info := &relaycommon.RelayInfo{
		OriginModelName:   "model-a",
		UsingGroup:        "group-a",
		StartTime:         start,
		FirstResponseTime: start.Add(100 * time.Millisecond),
		IsStream:          true,
	}

	sample := SnapshotRelaySample(info)
	info.OriginModelName = "mutated"
	info.UsingGroup = "mutated"
	info.StartTime = time.Now()

	assert.Equal(t, "model-a", sample.Model)
	assert.Equal(t, "group-a", sample.Group)
	assert.Positive(t, sample.LatencyMs)
	assert.True(t, sample.HasTtft)
	assert.Positive(t, sample.TtftMs)
}

func TestSnapshotRelaySampleNil(t *testing.T) {
	assert.Equal(t, Sample{}, SnapshotRelaySample(nil))
}

func TestEnqueueRelaySampleDropsAndCountsWhenFull(t *testing.T) {
	queue := make(chan Sample, 1)
	queue <- Sample{Model: "queued"}
	before := RelaySampleDroppedTotal()

	require.False(t, enqueueRelaySample(queue, Sample{Model: "dropped"}))
	assert.Equal(t, before+1, RelaySampleDroppedTotal())
}

func TestEnqueueRelaySampleAcceptsWithinCapacity(t *testing.T) {
	queue := make(chan Sample, 1)
	before := RelaySampleDroppedTotal()

	require.True(t, enqueueRelaySample(queue, Sample{Model: "accepted"}))
	assert.Equal(t, before, RelaySampleDroppedTotal())
	assert.Equal(t, "accepted", (<-queue).Model)
}
