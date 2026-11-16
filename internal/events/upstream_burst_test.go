package events

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUpstreamBurstTriggersOncePerWindowedBurst(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	detector := NewUpstreamBurstDetector(UpstreamBurstConfig{Threshold: 3, Window: time.Minute})

	for i := 0; i < 2; i++ {
		result := detector.ObserveAt("channel:7", http.StatusBadGateway, base.Add(time.Duration(i)*time.Second))
		assert.False(t, result.Triggered)
	}
	result := detector.ObserveAt("channel:7", http.StatusInternalServerError, base.Add(2*time.Second))
	assert.True(t, result.Triggered)
	assert.Equal(t, 3, result.Count)

	result = detector.ObserveAt("channel:7", http.StatusGatewayTimeout, base.Add(3*time.Second))
	assert.False(t, result.Triggered, "one burst must produce one notification")
	assert.Equal(t, 4, result.Count)
}

func TestUpstreamBurstRearmsAfterWindowFallsBelowThreshold(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	detector := NewUpstreamBurstDetector(UpstreamBurstConfig{Threshold: 2, Window: time.Minute})

	assert.False(t, detector.ObserveAt("channel:7", 500, base).Triggered)
	assert.True(t, detector.ObserveAt("channel:7", 500, base.Add(time.Second)).Triggered)

	// The old burst has fully expired; a non-5xx observation prunes it and
	// clears the armed state.
	result := detector.ObserveAt("channel:7", http.StatusOK, base.Add(62*time.Second))
	assert.False(t, result.Triggered)
	assert.Equal(t, 0, result.Count)

	assert.False(t, detector.ObserveAt("channel:7", 503, base.Add(63*time.Second)).Triggered)
	result = detector.ObserveAt("channel:7", 503, base.Add(64*time.Second))
	assert.True(t, result.Triggered)
	assert.Equal(t, 2, result.Count)
}

func TestUpstreamBurstIgnoresNon5xxAndSeparatesKeys(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	detector := NewUpstreamBurstDetector(UpstreamBurstConfig{Threshold: 2, Window: time.Minute})

	assert.False(t, detector.ObserveAt("channel:7", http.StatusTooManyRequests, base).Triggered)
	assert.False(t, detector.ObserveAt("channel:7", 499, base.Add(time.Second)).Triggered)
	assert.False(t, detector.ObserveAt("channel:8", 500, base.Add(2*time.Second)).Triggered)
	assert.False(t, detector.ObserveAt("channel:7", 500, base.Add(3*time.Second)).Triggered,
		"one 5xx for channel:7 is below threshold")
	assert.True(t, detector.ObserveAt("channel:7", 599, base.Add(4*time.Second)).Triggered)
}

func TestUpstreamBurstNormalizesInvalidConfig(t *testing.T) {
	detector := NewUpstreamBurstDetector(UpstreamBurstConfig{})
	config := detector.Config()
	assert.Equal(t, DefaultUpstreamBurstThreshold, config.Threshold)
	assert.Equal(t, DefaultUpstreamBurstWindow, config.Window)
}

func TestUpstreamBurstPruneRemovesQuietKeys(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	detector := NewUpstreamBurstDetector(UpstreamBurstConfig{Threshold: 2, Window: time.Minute})
	detector.ObserveAt("quiet", 500, base)
	detector.ObserveAt("active", 500, base.Add(30*time.Second))

	detector.PruneAt(base.Add(61 * time.Second))
	assert.False(t, detector.ObserveAt("quiet", 500, base.Add(62*time.Second)).Triggered)
	assert.True(t, detector.ObserveAt("active", 500, base.Add(62*time.Second)).Triggered)
}
