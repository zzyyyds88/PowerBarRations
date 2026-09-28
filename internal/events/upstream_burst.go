package events

import (
	"net/http"
	"sync"
	"time"
)

// DefaultUpstreamBurstThreshold and DefaultUpstreamBurstWindow are the
// conservative defaults for upstream 5xx burst detection. The owner that
// exposes system options should pass the effective values to the detector.
const (
	DefaultUpstreamBurstThreshold = 5
	DefaultUpstreamBurstWindow    = time.Minute
)

// UpstreamBurstConfig controls the per-key 5xx burst detector.
type UpstreamBurstConfig struct {
	Threshold int
	Window    time.Duration
}

func (c UpstreamBurstConfig) normalized() UpstreamBurstConfig {
	if c.Threshold <= 0 {
		c.Threshold = DefaultUpstreamBurstThreshold
	}
	if c.Window <= 0 {
		c.Window = DefaultUpstreamBurstWindow
	}
	return c
}

// NormalizedConfig returns a config with invalid or missing values replaced by
// the documented defaults.
func (c UpstreamBurstConfig) NormalizedConfig() UpstreamBurstConfig {
	return c.normalized()
}

// UpstreamBurstResult describes the state after recording one result.
// Triggered is true only when this observation crosses the threshold from
// below. Callers can publish one upstream_burst event for that transition;
// repeated failures in the same burst remain suppressed.
type UpstreamBurstResult struct {
	Key       string
	Count     int
	Threshold int
	Window    time.Duration
	Triggered bool
}

// UpstreamBurstDetector tracks 5xx timestamps independently for each key.
// A key should identify the operational subject being alerted (normally a
// channel ID); callers that need per-model alerts can include the model in it.
// The detector is process-local. Failure timestamps are bounded per key by the
// configured window; callers with a high-cardinality key space should invoke
// PruneAt periodically to remove keys that have gone quiet.
type UpstreamBurstDetector struct {
	mu       sync.Mutex
	config   UpstreamBurstConfig
	failures map[string][]time.Time
	armed    map[string]bool
}

const maxTrackedBurstKeys = 4096

// NewUpstreamBurstDetector creates a detector with normalized configuration.
func NewUpstreamBurstDetector(config UpstreamBurstConfig) *UpstreamBurstDetector {
	return &UpstreamBurstDetector{
		config:   config.normalized(),
		failures: make(map[string][]time.Time),
		armed:    make(map[string]bool),
	}
}

// Observe records an HTTP result using the current wall clock.
func (d *UpstreamBurstDetector) Observe(key string, statusCode int) UpstreamBurstResult {
	return d.ObserveAt(key, statusCode, time.Now())
}

// ObserveAt is Observe with an explicit timestamp, allowing deterministic
// tests and a single clock boundary in callers that already have one.
func (d *UpstreamBurstDetector) ObserveAt(key string, statusCode int, at time.Time) UpstreamBurstResult {
	if d == nil || key == "" {
		return UpstreamBurstResult{}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := at.Add(-d.config.Window)
	timestamps := d.failures[key]
	first := 0
	for first < len(timestamps) && !timestamps[first].After(cutoff) {
		first++
	}
	if first > 0 {
		timestamps = timestamps[first:]
	}

	if statusCode >= http.StatusInternalServerError && statusCode <= 599 {
		timestamps = append(timestamps, at)
	}

	count := len(timestamps)
	wasArmed := d.armed[key]
	triggered := !wasArmed && count >= d.config.Threshold
	if count >= d.config.Threshold {
		d.armed[key] = true
	} else {
		delete(d.armed, key)
	}

	if count == 0 {
		delete(d.failures, key)
	} else {
		d.failures[key] = timestamps
	}
	if len(d.failures) > maxTrackedBurstKeys {
		d.pruneLocked(at)
	}

	return UpstreamBurstResult{
		Key:       key,
		Count:     count,
		Threshold: d.config.Threshold,
		Window:    d.config.Window,
		Triggered: triggered,
	}
}

// Config returns the normalized detector configuration.
func (d *UpstreamBurstDetector) Config() UpstreamBurstConfig {
	if d == nil {
		return UpstreamBurstConfig{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.config
}

// PruneAt removes keys with no failures left in the configured window. It is
// safe to call from a maintenance ticker while Observe runs concurrently.
func (d *UpstreamBurstDetector) PruneAt(at time.Time) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pruneLocked(at)
}

func (d *UpstreamBurstDetector) pruneLocked(at time.Time) {
	cutoff := at.Add(-d.config.Window)
	for key, timestamps := range d.failures {
		first := 0
		for first < len(timestamps) && !timestamps[first].After(cutoff) {
			first++
		}
		if first == len(timestamps) {
			delete(d.failures, key)
			delete(d.armed, key)
			continue
		}
		if first > 0 {
			d.failures[key] = timestamps[first:]
		}
	}
	for len(d.failures) > maxTrackedBurstKeys {
		for key := range d.failures {
			delete(d.failures, key)
			delete(d.armed, key)
			break
		}
	}
}
