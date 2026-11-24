package route

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zzyyyds88/PowerBarRations/internal/events"
)

// admissionNow 可被测试替换的时钟（与 nowMs 分开：准入窗口用 wall clock 秒）。
var admissionNow = time.Now

// 出站准入控制（routing-spec §3.2、ADR 0012）。
//
// 作用域 = (channelId, member.Model)，进程内全局共享（跨车道/成员组），重启清空。
// 两个正交上限：max_concurrency（在飞）与 max_rpm（滑动 60s 尝试数）；
// max_concurrency = -1 时由 AIMD 自学习。排队是 FIFO，就绪成员优先由选路层
// 的 admissionReady 回调保证（State 跳过未就绪成员，先给低优先级但就绪的成员）。

// RateLimitConfig 是一行渠道准入配置的运行态形态。
type RateLimitConfig struct {
	MaxConcurrency   int
	MaxRPM           int
	QueueLimit       int
	QueueWaitSeconds int
}

const (
	adaptiveConcurrencyCeiling = 32
	adaptiveIdleDecay          = 5 * time.Minute
	admissionEventWindow       = time.Minute
)

var (
	// ErrAdmissionNotReady 不排队模式下成员不可立即就绪。
	ErrAdmissionNotReady = errors.New("admission: member not ready")
	// ErrAdmissionQueueFull 该成员排队人数已达 queue_limit。
	ErrAdmissionQueueFull = errors.New("admission: member queue full")
	// ErrAdmissionWaitTimeout 排队超过 queue_wait_seconds。
	ErrAdmissionWaitTimeout = errors.New("admission: queue wait timed out")
)

// AdmissionRegistry 进程级准入运行态。
type AdmissionRegistry struct {
	mu      sync.Mutex
	members map[string]*admissionMember
}

// DefaultAdmission 是进程级准入注册表。
var DefaultAdmission = &AdmissionRegistry{members: map[string]*admissionMember{}}

// AdmissionKey 运行态键：渠道 id + 成员所选模型（配置也按所选模型寻址）。
func AdmissionKey(channelId int, modelName string) string {
	return strconv.Itoa(channelId) + ":" + strings.TrimSpace(modelName)
}

type admissionMember struct {
	inflight    int
	limit       float64 // 自适应上限
	eventKey    string
	initialized bool
	rpm         []int64 // Unix 秒，滑动 60s
	waiters     []*admissionWaiter
	pausedUntil time.Time
	lastActive  time.Time
	ttftBaseMs  float64
	ttftSamples int
	adaptive    bool
	laneName    string
	lastEvent   map[string]time.Time
}

type admissionWaiter struct {
	ready chan struct{}
}

// AdmissionTicket 一次已获准发往上游的槽位；Release 幂等。
type AdmissionTicket struct {
	registry   *AdmissionRegistry
	key        string
	config     RateLimitConfig
	saturated  bool
	adaptive   bool
	acquiredAt time.Time

	mu       sync.Mutex
	released bool
}

// Saturated 取槽时是否已打满当前上限（AIMD 加性增长的前提）。
func (t *AdmissionTicket) Saturated() bool {
	if t == nil {
		return false
	}
	return t.saturated
}

// Key 运行态键（日志/排障用）。
func (t *AdmissionTicket) Key() string {
	if t == nil {
		return ""
	}
	return t.key
}

// Release 释放槽位并按本次结果更新自适应上限；幂等。
func (t *AdmissionTicket) Release(kind ErrorKind, ttftMs int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.released {
		t.mu.Unlock()
		return
	}
	t.released = true
	t.mu.Unlock()

	reg := t.registry
	reg.mu.Lock()
	defer reg.mu.Unlock()
	m := reg.members[t.key]
	if m == nil {
		return
	}
	if m.inflight > 0 {
		m.inflight--
	}
	now := admissionNow()
	if t.adaptive {
		reg.applyAIMDLocked(t.key, m, kind, ttftMs, t.saturated, now)
	}
	if m.inflight == 0 && len(m.waiters) == 0 {
		m.lastActive = now
	}
	reg.signalHeadLocked(m)
}

// Ready 非阻塞判断该成员的配置当前是否就绪（供选路回调；不取槽）。
func (a *AdmissionRegistry) Ready(key string, cfg RateLimitConfig) bool {
	if !cfg.Enabled() {
		return true
	}
	now := admissionNow()
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.memberLocked(key, cfg)
	a.decayIfIdleLocked(m, cfg, now)
	a.pruneRPMLocked(m, now)
	return a.canTakeLocked(m, cfg, now) && len(m.waiters) == 0
}

// Stats 返回该键的准入运行态（健康快照用）。
type AdmissionStats struct {
	Inflight       int
	Queued         int
	RPMUsed        int
	Adaptive       bool
	AdaptiveLimit  float64
	TTFTBaselineMs float64
}

func (a *AdmissionRegistry) Stats(key string) AdmissionStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.members[key]
	if m == nil {
		return AdmissionStats{}
	}
	a.pruneRPMLocked(m, admissionNow())
	return AdmissionStats{
		Inflight:       m.inflight,
		Queued:         len(m.waiters),
		RPMUsed:        len(m.rpm),
		Adaptive:       m.adaptive,
		AdaptiveLimit:  m.limit,
		TTFTBaselineMs: m.ttftBaseMs,
	}
}

// Acquire 非阻塞取槽：不就绪返回 ErrAdmissionNotReady（不排队）。
func (a *AdmissionRegistry) Acquire(key string, cfg RateLimitConfig) (*AdmissionTicket, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	now := admissionNow()
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.memberLocked(key, cfg)
	a.decayIfIdleLocked(m, cfg, now)
	a.pruneRPMLocked(m, now)
	if !a.canTakeLocked(m, cfg, now) || len(m.waiters) > 0 {
		return nil, ErrAdmissionNotReady
	}
	return a.takeLocked(key, m, cfg, now), nil
}

// Wait 进入该成员 FIFO 队列，直到就绪、超时、取消或队列满。
// 返回的建议等待时长用于 Retry-After 与 attempts.wait_ms。
func (a *AdmissionRegistry) Wait(ctx context.Context, key, lane string, cfg RateLimitConfig) (*AdmissionTicket, time.Duration, error) {
	if !cfg.Enabled() {
		return nil, 0, nil
	}
	if cfg.QueueWaitSeconds <= 0 {
		return nil, 0, ErrAdmissionNotReady
	}
	now := admissionNow()
	a.mu.Lock()
	m := a.memberLocked(key, cfg)
	a.decayIfIdleLocked(m, cfg, now)
	a.pruneRPMLocked(m, now)
	if m.laneName == "" {
		m.laneName = lane
	}
	if a.canTakeLocked(m, cfg, now) && len(m.waiters) == 0 {
		ticket := a.takeLocked(key, m, cfg, now)
		a.mu.Unlock()
		return ticket, 0, nil
	}
	limit := cfg.QueueLimit
	if limit <= 0 {
		limit = 32
	}
	if len(m.waiters) >= limit {
		estimate := a.estimateLocked(m, cfg, now)
		a.emitThrottledLocked(m, "queue_full", estimate, now)
		a.mu.Unlock()
		return nil, estimate, ErrAdmissionQueueFull
	}
	waiter := &admissionWaiter{ready: make(chan struct{}, 1)}
	m.waiters = append(m.waiters, waiter)
	a.mu.Unlock()

	deadline := time.NewTimer(time.Duration(cfg.QueueWaitSeconds) * time.Second)
	defer deadline.Stop()
	// 50ms 兜底轮询只用于 RPM 窗口滑出（没有"释放"事件可等）与
	// 队列头部状态变化；正常路径由 signalHead 立即唤醒。
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			a.removeWaiter(key, waiter)
			return nil, time.Since(start), ctx.Err()
		case <-deadline.C:
			a.removeWaiter(key, waiter)
			estimate := time.Duration(cfg.QueueWaitSeconds) * time.Second
			a.recordWaitOutcome(key, m, "queue_wait", estimate)
			return nil, estimate, ErrAdmissionWaitTimeout
		case <-waiter.ready:
		case <-ticker.C:
		}
		a.mu.Lock()
		m = a.members[key]
		if m == nil || !a.isHeadLocked(m, waiter) {
			a.mu.Unlock()
			continue
		}
		now = admissionNow()
		a.pruneRPMLocked(m, now)
		if a.canTakeLocked(m, cfg, now) {
			ticket := a.takeLocked(key, m, cfg, now)
			a.removeWaiterLocked(m, waiter)
			a.mu.Unlock()
			return ticket, time.Since(start), nil
		}
		a.mu.Unlock()
	}
}

// Estimate 预计该成员最早就绪时长（Retry-After / 候选排序用）。
func (a *AdmissionRegistry) Estimate(key string, cfg RateLimitConfig) time.Duration {
	if !cfg.Enabled() {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.memberLocked(key, cfg)
	now := admissionNow()
	a.pruneRPMLocked(m, now)
	return a.estimateLocked(m, cfg, now)
}

// Enabled 该配置是否真正限制（两个固定上限都为 0 = 不限）。
func (cfg RateLimitConfig) Enabled() bool {
	return cfg.MaxConcurrency != 0 || cfg.MaxRPM > 0
}

func (a *AdmissionRegistry) memberLocked(key string, cfg RateLimitConfig) *admissionMember {
	m := a.members[key]
	if m == nil {
		m = &admissionMember{lastEvent: map[string]time.Time{}, eventKey: key}
		a.members[key] = m
	}
	if cfg.MaxConcurrency == -1 {
		m.adaptive = true
		if !m.initialized {
			m.limit = math.Max(2, adaptiveConcurrencyCeiling/8)
			m.initialized = true
		}
	}
	return m
}

func (a *AdmissionRegistry) canTakeLocked(m *admissionMember, cfg RateLimitConfig, now time.Time) bool {
	if now.Before(m.pausedUntil) {
		return false
	}
	a.pruneRPMLocked(m, now)
	if cfg.MaxConcurrency > 0 || cfg.MaxConcurrency == -1 {
		limit := float64(cfg.MaxConcurrency)
		if cfg.MaxConcurrency == -1 {
			limit = m.limit
		}
		if float64(m.inflight) >= limit {
			return false
		}
	}
	if cfg.MaxRPM > 0 && len(m.rpm) >= cfg.MaxRPM {
		return false
	}
	return true
}

func (a *AdmissionRegistry) takeLocked(key string, m *admissionMember, cfg RateLimitConfig, now time.Time) *AdmissionTicket {
	m.inflight++
	if cfg.MaxRPM > 0 {
		m.rpm = append(m.rpm, now.Unix())
	}
	m.lastActive = now
	limit := float64(cfg.MaxConcurrency)
	if cfg.MaxConcurrency == -1 {
		limit = m.limit
	}
	saturated := limit > 0 && float64(m.inflight) >= limit
	return &AdmissionTicket{
		registry:   a,
		key:        key,
		config:     cfg,
		saturated:  saturated,
		adaptive:   cfg.MaxConcurrency == -1,
		acquiredAt: now,
	}
}

func (a *AdmissionRegistry) pruneRPMLocked(m *admissionMember, now time.Time) {
	if len(m.rpm) == 0 {
		return
	}
	cutoff := now.Unix() - 60
	kept := m.rpm[:0]
	for _, ts := range m.rpm {
		if ts > cutoff {
			kept = append(kept, ts)
		}
	}
	m.rpm = kept
}

func (a *AdmissionRegistry) estimateLocked(m *admissionMember, cfg RateLimitConfig, now time.Time) time.Duration {
	var wait time.Duration
	if now.Before(m.pausedUntil) {
		wait = m.pausedUntil.Sub(now)
	}
	if cfg.MaxConcurrency > 0 || cfg.MaxConcurrency == -1 {
		limit := float64(cfg.MaxConcurrency)
		if cfg.MaxConcurrency == -1 {
			limit = m.limit
		}
		if limit > 0 && float64(m.inflight) >= limit {
			// 槽位空出时间未知：给 1 秒保守估计（ADR 0012 的"最早就绪估计"）。
			if wait < time.Second {
				wait = time.Second
			}
		}
	}
	if cfg.MaxRPM > 0 && len(m.rpm) >= cfg.MaxRPM {
		index := len(m.rpm) - cfg.MaxRPM
		if index >= 0 {
			slide := time.Unix(m.rpm[index]+61, 0).Sub(now)
			if slide > wait {
				wait = slide
			}
		}
	}
	if wait < time.Second {
		wait = time.Second
	}
	return wait
}

func (a *AdmissionRegistry) signalHeadLocked(m *admissionMember) {
	if len(m.waiters) == 0 {
		return
	}
	select {
	case m.waiters[0].ready <- struct{}{}:
	default:
	}
}

func (a *AdmissionRegistry) isHeadLocked(m *admissionMember, waiter *admissionWaiter) bool {
	return len(m.waiters) > 0 && m.waiters[0] == waiter
}

func (a *AdmissionRegistry) removeWaiter(key string, waiter *admissionWaiter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if m := a.members[key]; m != nil {
		a.removeWaiterLocked(m, waiter)
	}
}

func (a *AdmissionRegistry) removeWaiterLocked(m *admissionMember, waiter *admissionWaiter) {
	for i, item := range m.waiters {
		if item == waiter {
			m.waiters = append(m.waiters[:i], m.waiters[i+1:]...)
			break
		}
	}
	a.signalHeadLocked(m)
}

func (a *AdmissionRegistry) decayIfIdleLocked(m *admissionMember, cfg RateLimitConfig, now time.Time) {
	if !m.adaptive || m.lastActive.IsZero() || now.Sub(m.lastActive) < adaptiveIdleDecay {
		return
	}
	initial := math.Max(2, adaptiveConcurrencyCeiling/8)
	if m.limit > initial {
		m.limit = math.Max(initial, math.Floor(m.limit/2))
		a.emitLimitLocked(m, "idle_decay", now)
	}
	m.lastActive = now
}

func (a *AdmissionRegistry) applyAIMDLocked(key string, m *admissionMember, kind ErrorKind, ttftMs int64, saturated bool, now time.Time) {
	switch kind {
	case KindSoftRateLimit:
		m.limit = math.Max(1, math.Floor(m.limit*0.5))
		m.pausedUntil = now.Add(time.Second)
		a.emitLimitLocked(m, "rate_limited", now)
	case KindTimeout:
		m.limit = math.Max(1, math.Floor(m.limit*0.9))
		a.emitLimitLocked(m, "timeout", now)
	default:
		if kind == "" {
			// 成功：先更新 TTFT 基线（p50 EWMA，α=0.2）。
			if ttftMs > 0 {
				if m.ttftSamples == 0 {
					m.ttftBaseMs = float64(ttftMs)
				} else {
					m.ttftBaseMs = 0.8*m.ttftBaseMs + 0.2*float64(ttftMs)
				}
				m.ttftSamples++
			}
			// 延迟辅助：TTFT 超过 2× 基线且已打满 → 降档；否则加性增长。
			if saturated && m.ttftBaseMs > 0 && ttftMs > 0 && float64(ttftMs) > 2*m.ttftBaseMs {
				m.limit = math.Max(1, math.Floor(m.limit*0.9))
				a.emitLimitLocked(m, "latency", now)
				return
			}
			if saturated {
				m.limit = math.Min(adaptiveConcurrencyCeiling, m.limit+1/m.limit)
				a.emitLimitLocked(m, "success", now)
			}
		}
	}
}

func (a *AdmissionRegistry) recordWaitOutcome(key string, m *admissionMember, reason string, wait time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if current := a.members[key]; current != nil {
		a.emitThrottledLocked(current, reason, wait, admissionNow())
	}
}

func (a *AdmissionRegistry) emitThrottledLocked(m *admissionMember, reason string, wait time.Duration, now time.Time) {
	a.emitOnceLocked(m, events.EventMemberThrottled, now,
		"reason="+reason+" wait_ms="+strconv.FormatInt(wait.Milliseconds(), 10))
}

func (a *AdmissionRegistry) emitLimitLocked(m *admissionMember, reason string, now time.Time) {
	a.emitOnceLocked(m, EventAdaptiveLimitChanged, now,
		"limit="+strconv.FormatFloat(m.limit, 'f', -1, 64)+" reason="+reason)
}

func (a *AdmissionRegistry) emitOnceLocked(m *admissionMember, eventType string, now time.Time, detail string) {
	if m.lastEvent == nil {
		m.lastEvent = map[string]time.Time{}
	}
	if last, ok := m.lastEvent[eventType]; ok && now.Sub(last) < admissionEventWindow {
		return
	}
	m.lastEvent[eventType] = now
	events.Publish(Event{
		Ts: now.UnixMilli(), Type: eventType, Lane: m.laneName,
		Member: eventMember(m.eventKey), Detail: detail,
	})
}
