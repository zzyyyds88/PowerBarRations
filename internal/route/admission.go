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
	"github.com/zzyyyds88/PowerBarRations/model"
)

// admissionNow 可被测试替换的时钟（与 nowMs 分开：准入窗口用 wall clock 秒）。
var admissionNow = time.Now

// RateLimitConfig 是一行渠道准入配置的运行态形态。Mode 必须由配置解析层显式设置，
// 避免用 max_concurrency 的零值间接推断产品语义。
type RateLimitConfig struct {
	Mode             AdmissionMode
	MaxConcurrency   int
	MaxRPM           int
	QueueLimit       int
	QueueWaitSeconds int
}

// AdmissionMode 是准入配置的三种显式模式。
type AdmissionMode uint8

const (
	AdmissionModeDisabled AdmissionMode = iota
	AdmissionModeFixed
	AdmissionModeAdaptive
)

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
	// ErrAdmissionWaitTimeout 排队超过请求级或成员级准入 deadline。
	ErrAdmissionWaitTimeout = errors.New("admission: queue wait timed out")
	// ErrAdmissionInvalidated 配置删除、模型移除或代次变化使 waiter 失效。
	ErrAdmissionInvalidated = errors.New("admission: waiter invalidated")
)

// AdmissionKey 运行态键：渠道 id + 成员所选模型（配置也按所选模型寻址）。
type AdmissionKey struct {
	ChannelID int
	Model     string
}

// AdmissionKeyOf 从解析后的成员建立类型化准入键。
func AdmissionKeyOf(member *model.RouteMember) AdmissionKey {
	if member == nil {
		return AdmissionKey{}
	}
	return AdmissionKey{ChannelID: member.ChannelId, Model: strings.TrimSpace(member.Model)}
}

func (key AdmissionKey) String() string {
	return strconv.Itoa(key.ChannelID) + ":" + strings.TrimSpace(key.Model)
}

// EventMember 是准入事件的对外成员投影。
func (key AdmissionKey) EventMember() string { return key.String() }

// Normalize 返回显式模式下的规范化配置。
func (cfg RateLimitConfig) Normalize() RateLimitConfig {
	switch cfg.Mode {
	case AdmissionModeAdaptive:
		cfg.MaxConcurrency = -1
	case AdmissionModeFixed:
		if cfg.MaxConcurrency < 0 {
			cfg.MaxConcurrency = 0
		}
	case AdmissionModeDisabled:
		cfg.MaxConcurrency = 0
		cfg.MaxRPM = 0
		cfg.QueueWaitSeconds = 0
	}
	if cfg.QueueLimit <= 0 {
		cfg.QueueLimit = 32
	}
	return cfg
}

// Enabled 该配置是否真正限制。
func (cfg RateLimitConfig) Enabled() bool { return cfg.Mode != AdmissionModeDisabled }

// AdmissionOutcome 是一次准入槽位的最终结果。neutral/canceled 不参与 AIMD。
type AdmissionOutcome uint8

const (
	AdmissionNeutral AdmissionOutcome = iota
	AdmissionSuccess
	AdmissionFailure
	AdmissionCanceled
)

// AdmissionOutcomeOf 把准入等待错误映射为运行态 outcome。它只用于释放语义，
// 不替代上游错误分类。
func AdmissionOutcomeOf(err error) AdmissionOutcome {
	switch {
	case err == nil:
		return AdmissionSuccess
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return AdmissionCanceled
	case errors.Is(err, ErrAdmissionNotReady), errors.Is(err, ErrAdmissionQueueFull),
		errors.Is(err, ErrAdmissionWaitTimeout), errors.Is(err, ErrAdmissionInvalidated):
		return AdmissionNeutral
	default:
		return AdmissionFailure
	}
}

// AdmissionRegistry 进程级准入运行态。
type AdmissionRegistry struct {
	mu      sync.Mutex
	members map[AdmissionKey]*admissionMember
}

// DefaultAdmission 是进程级准入注册表。
var DefaultAdmission = &AdmissionRegistry{members: map[AdmissionKey]*admissionMember{}}

type admissionMember struct {
	inflight    int
	limit       float64 // 自适应上限
	initialized bool
	rpm         []int64 // Unix 秒，滑动 60s
	waiters     []*admissionWaiter
	pausedUntil time.Time
	lastActive  time.Time
	ttftBaseMs  float64
	ttftSamples int
	mode        AdmissionMode
	retired     bool
	epoch       uint64
	laneName    string
	label       string
	lastEvent   map[string]time.Time
}

type admissionWaiter struct {
	ready chan struct{}
	err   error // 仅在 AdmissionRegistry.mu 下访问
}

// AdmissionTicket 一次已获准发往上游的槽位；Release 幂等。
type AdmissionTicket struct {
	registry   *AdmissionRegistry
	key        AdmissionKey
	config     RateLimitConfig
	epoch      uint64
	saturated  bool
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
func (t *AdmissionTicket) Key() AdmissionKey {
	if t == nil {
		return AdmissionKey{}
	}
	return t.key
}

// Release 释放槽位并按本次结果更新自适应上限；幂等。neutral/canceled 只释放资源。
func (t *AdmissionTicket) Release(outcome AdmissionOutcome, kind ErrorKind, ttftMs int64) {
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
	if reg == nil {
		return
	}
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
	if t.config.Mode == AdmissionModeAdaptive && m.mode == AdmissionModeAdaptive && t.epoch == m.epoch {
		switch outcome {
		case AdmissionSuccess:
			reg.applyAIMDSuccessLocked(m, ttftMs, t.saturated, now)
		case AdmissionFailure:
			reg.applyAIMDFailureLocked(m, kind, now)
		}
	}
	if m.inflight == 0 && len(m.waiters) == 0 {
		m.lastActive = now
		if m.retired {
			delete(reg.members, t.key)
		}
	}
	reg.signalHeadLocked(m)
}

// Ready 非阻塞判断该成员的配置当前是否就绪（供选路回调；不取槽）。
func (a *AdmissionRegistry) Ready(key AdmissionKey, cfg RateLimitConfig) bool {
	cfg = cfg.Normalize()
	if !cfg.Enabled() {
		a.mu.Lock()
		a.observeDisabledLocked(key)
		a.mu.Unlock()
		return true
	}
	now := admissionNow()
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.memberLocked(key, cfg)
	a.decayIfIdleLocked(m, now)
	a.pruneRPMLocked(m, now)
	return a.canTakeLocked(m, cfg, now) && len(m.waiters) == 0
}

// AdmissionStats 返回该键的准入运行态（健康快照用）。Mode/Adaptive 按当前 cfg 报告，
// 不保留上一次配置的模式残影。
type AdmissionStats struct {
	Inflight       int
	Queued         int
	RPMUsed        int
	Mode           AdmissionMode
	Adaptive       bool
	AdaptiveLimit  float64
	TTFTBaselineMs float64
}

func (a *AdmissionRegistry) Stats(key AdmissionKey, configs ...RateLimitConfig) AdmissionStats {
	cfg := RateLimitConfig{Mode: AdmissionModeDisabled}
	if len(configs) > 0 {
		cfg = configs[0].Normalize()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.members[key]
	if m == nil {
		return AdmissionStats{Mode: cfg.Mode}
	}
	// Stats 是健康/SSE 的只读投影：不能在这里 prune RPM 窗口。
	rpmUsed := 0
	cutoff := admissionNow().Unix() - 60
	for _, ts := range m.rpm {
		if ts > cutoff {
			rpmUsed++
		}
	}
	mode := cfg.Mode
	if len(configs) == 0 {
		mode = m.mode
	}
	stats := AdmissionStats{
		Inflight: m.inflight,
		Queued:   len(m.waiters),
		RPMUsed:  rpmUsed,
		Mode:     mode,
		Adaptive: mode == AdmissionModeAdaptive,
	}
	if stats.Adaptive {
		stats.AdaptiveLimit = m.limit
		stats.TTFTBaselineMs = m.ttftBaseMs
	}
	return stats
}

// Acquire 非阻塞取槽：不就绪返回 ErrAdmissionNotReady（不排队）。
func (a *AdmissionRegistry) Acquire(key AdmissionKey, cfg RateLimitConfig) (*AdmissionTicket, error) {
	cfg = cfg.Normalize()
	if !cfg.Enabled() {
		a.mu.Lock()
		a.observeDisabledLocked(key)
		a.mu.Unlock()
		return nil, nil
	}
	now := admissionNow()
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.memberLocked(key, cfg)
	a.decayIfIdleLocked(m, now)
	a.pruneRPMLocked(m, now)
	if !a.canTakeLocked(m, cfg, now) || len(m.waiters) > 0 {
		return nil, ErrAdmissionNotReady
	}
	return a.takeLocked(key, m, cfg, now), nil
}

// Wait 进入该成员 FIFO 队列，直到就绪、超时、取消、失效或队列满。
// 可选的 requestDeadline 是请求级绝对 deadline，只约束排队；成员 queue_wait_seconds
// 只能缩短它。取得槽位后上游尝试不再受该 deadline 约束。
func (a *AdmissionRegistry) Wait(ctx context.Context, key AdmissionKey, lane string, cfg RateLimitConfig, requestDeadline ...time.Time) (*AdmissionTicket, time.Duration, error) {
	cfg = cfg.Normalize()
	if !cfg.Enabled() {
		a.mu.Lock()
		a.observeDisabledLocked(key)
		a.mu.Unlock()
		return nil, 0, nil
	}
	if cfg.QueueWaitSeconds <= 0 {
		return nil, 0, ErrAdmissionNotReady
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	start := time.Now()
	deadline := start.Add(time.Duration(cfg.QueueWaitSeconds) * time.Second)
	if len(requestDeadline) > 0 && !requestDeadline[0].IsZero() && requestDeadline[0].Before(deadline) {
		deadline = requestDeadline[0]
	}
	if !time.Now().Before(deadline) {
		return nil, 0, ErrAdmissionWaitTimeout
	}

	now := admissionNow()
	a.mu.Lock()
	m := a.memberLocked(key, cfg)
	a.decayIfIdleLocked(m, now)
	a.pruneRPMLocked(m, now)
	if m.laneName == "" {
		m.laneName = lane
	}
	if a.canTakeLocked(m, cfg, now) && len(m.waiters) == 0 {
		ticket := a.takeLocked(key, m, cfg, now)
		a.mu.Unlock()
		return ticket, 0, nil
	}
	if len(m.waiters) >= cfg.QueueLimit {
		estimate := a.estimateLocked(m, cfg, now)
		a.emitThrottledLocked(m, "queue_full", estimate, now)
		a.mu.Unlock()
		return nil, estimate, ErrAdmissionQueueFull
	}
	waiter := &admissionWaiter{ready: make(chan struct{}, 1)}
	m.waiters = append(m.waiters, waiter)
	initialWait := a.nextWaitDurationLocked(m, cfg, deadline)
	a.mu.Unlock()

	timer := time.NewTimer(initialWait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			a.removeWaiter(key, waiter)
			return nil, time.Since(start), ctx.Err()
		case <-timer.C:
		case <-waiter.ready:
		}

		a.mu.Lock()
		if waiter.err != nil {
			err := waiter.err
			a.removeWaiterLocked(m, waiter)
			a.mu.Unlock()
			return nil, time.Since(start), err
		}
		// 取消与 ready 竞态时，取槽前必须再次检查 ctx 与 deadline。
		if err := ctx.Err(); err != nil {
			a.removeWaiterLocked(m, waiter)
			a.mu.Unlock()
			return nil, time.Since(start), err
		}
		if !time.Now().Before(deadline) {
			a.removeWaiterLocked(m, waiter)
			estimate := time.Until(deadline)
			if estimate < 0 {
				estimate = 0
			}
			a.emitThrottledLocked(m, "queue_wait", estimate, admissionNow())
			a.mu.Unlock()
			return nil, time.Since(start), ErrAdmissionWaitTimeout
		}
		m = a.members[key]
		if m == nil {
			a.mu.Unlock()
			return nil, time.Since(start), ErrAdmissionInvalidated
		}
		if !a.isHeadLocked(m, waiter) {
			wait := a.nextWaitDurationLocked(m, cfg, deadline)
			a.mu.Unlock()
			resetAdmissionTimer(timer, wait)
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
		wait := a.nextWaitDurationLocked(m, cfg, deadline)
		a.mu.Unlock()
		resetAdmissionTimer(timer, wait)
	}
}

func resetAdmissionTimer(timer *time.Timer, duration time.Duration) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

// nextWaitDurationLocked 计算下一次状态检查时长：优先请求/成员 deadline，其次是暂停
// 到期或 RPM 窗口滑出。没有可计算事件时用 1 秒保守兜底，不再每 waiter 50ms 轮询。
// 调用方必须持有 a.mu；m 必须仍属于当前 key。
func (a *AdmissionRegistry) nextWaitDurationLocked(m *admissionMember, cfg RateLimitConfig, deadline time.Time) time.Duration {
	now := admissionNow()
	wait := time.Duration(0)
	if m.pausedUntil.After(now) {
		wait = m.pausedUntil.Sub(now)
	}
	if cfg.MaxConcurrency > 0 || cfg.Mode == AdmissionModeAdaptive {
		limit := float64(cfg.MaxConcurrency)
		if cfg.Mode == AdmissionModeAdaptive {
			limit = m.limit
		}
		if limit > 0 && float64(m.inflight) >= limit && wait < time.Second {
			wait = time.Second
		}
	}
	if cfg.MaxRPM > 0 {
		a.pruneRPMLocked(m, now)
		if len(m.rpm) >= cfg.MaxRPM {
			index := len(m.rpm) - cfg.MaxRPM
			if index >= 0 {
				slide := time.Unix(m.rpm[index]+61, 0).Sub(now)
				if slide > wait {
					wait = slide
				}
			}
		}
	}
	if wait <= 0 {
		wait = time.Second
	}
	if until := time.Until(deadline); until <= 0 {
		return time.Nanosecond
	} else if until < wait {
		return until
	}
	return wait
}

// Estimate 预计该成员最早就绪时长（Retry-After / 候选排序用）。
func (a *AdmissionRegistry) Estimate(key AdmissionKey, cfg RateLimitConfig) time.Duration {
	cfg = cfg.Normalize()
	if !cfg.Enabled() {
		a.mu.Lock()
		a.observeDisabledLocked(key)
		a.mu.Unlock()
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.memberLocked(key, cfg)
	now := admissionNow()
	a.pruneRPMLocked(m, now)
	return a.estimateLocked(m, cfg, now)
}

// Invalidate 使该键所有 waiter 失效并唤醒。仅当没有在飞请求也没有 waiter 时删除
// 运行态；仍有在飞请求时保留计数，避免旧 ticket 释放污染新状态。
func (a *AdmissionRegistry) Invalidate(key AdmissionKey) {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.members[key]
	if m == nil {
		return
	}
	m.epoch++
	for _, waiter := range m.waiters {
		waiter.err = ErrAdmissionInvalidated
		a.notifyWaiterLocked(waiter)
	}
	m.waiters = nil
	m.retired = true
	if m.inflight == 0 {
		delete(a.members, key)
	}
}

// DeleteIdle 仅在 inflight=0 且 waiters=0 时删除运行态。
func (a *AdmissionRegistry) DeleteIdle(key AdmissionKey) {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.members[key]
	if m != nil && m.inflight == 0 && len(m.waiters) == 0 {
		delete(a.members, key)
	}
}

func (a *AdmissionRegistry) observeDisabledLocked(key AdmissionKey) {
	m := a.members[key]
	if m == nil {
		return
	}
	if m.mode == AdmissionModeAdaptive {
		m.pausedUntil = time.Time{}
	}
	m.mode = AdmissionModeDisabled
}

func (a *AdmissionRegistry) memberLocked(key AdmissionKey, cfg RateLimitConfig) *admissionMember {
	m := a.members[key]
	if m == nil {
		m = &admissionMember{lastEvent: map[string]time.Time{}, label: key.String()}
		a.members[key] = m
	}
	if m.mode != cfg.Mode {
		m.epoch++
		for _, waiter := range m.waiters {
			waiter.err = ErrAdmissionInvalidated
			a.notifyWaiterLocked(waiter)
		}
		m.waiters = nil
		if m.mode == AdmissionModeAdaptive && cfg.Mode != AdmissionModeAdaptive {
			m.pausedUntil = time.Time{}
		}
		m.mode = cfg.Mode
	}
	m.retired = false
	if m.mode == AdmissionModeAdaptive && !m.initialized {
		m.limit = math.Max(2, adaptiveConcurrencyCeiling/8)
		m.initialized = true
	}
	return m
}

func (a *AdmissionRegistry) canTakeLocked(m *admissionMember, cfg RateLimitConfig, now time.Time) bool {
	if now.Before(m.pausedUntil) {
		return false
	}
	a.pruneRPMLocked(m, now)
	if cfg.Mode == AdmissionModeFixed && cfg.MaxConcurrency > 0 {
		if float64(m.inflight) >= float64(cfg.MaxConcurrency) {
			return false
		}
	}
	if cfg.Mode == AdmissionModeAdaptive && float64(m.inflight) >= m.limit {
		return false
	}
	if cfg.MaxRPM > 0 && len(m.rpm) >= cfg.MaxRPM {
		return false
	}
	return true
}

func (a *AdmissionRegistry) takeLocked(key AdmissionKey, m *admissionMember, cfg RateLimitConfig, now time.Time) *AdmissionTicket {
	m.inflight++
	if cfg.MaxRPM > 0 {
		m.rpm = append(m.rpm, now.Unix())
	}
	m.lastActive = now
	limit := float64(0)
	switch cfg.Mode {
	case AdmissionModeFixed:
		limit = float64(cfg.MaxConcurrency)
	case AdmissionModeAdaptive:
		limit = m.limit
	}
	saturated := limit > 0 && float64(m.inflight) >= limit
	return &AdmissionTicket{
		registry:   a,
		key:        key,
		config:     cfg,
		epoch:      m.epoch,
		saturated:  saturated,
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
	if cfg.Mode == AdmissionModeFixed && cfg.MaxConcurrency > 0 {
		if float64(m.inflight) >= float64(cfg.MaxConcurrency) && wait < time.Second {
			wait = time.Second
		}
	}
	if cfg.Mode == AdmissionModeAdaptive && float64(m.inflight) >= m.limit && wait < time.Second {
		wait = time.Second
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
	a.notifyWaiterLocked(m.waiters[0])
}

func (a *AdmissionRegistry) notifyWaiterLocked(waiter *admissionWaiter) {
	if waiter == nil {
		return
	}
	select {
	case waiter.ready <- struct{}{}:
	default:
	}
}

func (a *AdmissionRegistry) isHeadLocked(m *admissionMember, waiter *admissionWaiter) bool {
	return len(m.waiters) > 0 && m.waiters[0] == waiter
}

func (a *AdmissionRegistry) removeWaiter(key AdmissionKey, waiter *admissionWaiter) {
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

func (a *AdmissionRegistry) decayIfIdleLocked(m *admissionMember, now time.Time) {
	if m.mode != AdmissionModeAdaptive || m.lastActive.IsZero() || now.Sub(m.lastActive) < adaptiveIdleDecay {
		return
	}
	initial := math.Max(2, adaptiveConcurrencyCeiling/8)
	if m.limit > initial {
		m.limit = math.Max(initial, math.Floor(m.limit/2))
		a.emitLimitLocked(m, "idle_decay", now)
	}
	m.lastActive = now
}

func (a *AdmissionRegistry) applyAIMDSuccessLocked(m *admissionMember, ttftMs int64, saturated bool, now time.Time) {
	oldBaseline := m.ttftBaseMs
	if ttftMs > 0 {
		if m.ttftSamples == 0 {
			m.ttftBaseMs = float64(ttftMs)
		} else {
			m.ttftBaseMs = 0.8*m.ttftBaseMs + 0.2*float64(ttftMs)
		}
		m.ttftSamples++
	}
	if saturated && oldBaseline > 0 && ttftMs > 0 && float64(ttftMs) > 2*oldBaseline {
		m.limit = math.Max(1, math.Floor(m.limit*0.9))
		a.emitLimitLocked(m, "latency", now)
		return
	}
	if saturated {
		m.limit = math.Min(adaptiveConcurrencyCeiling, m.limit+1/m.limit)
		a.emitLimitLocked(m, "success", now)
	}
}

func (a *AdmissionRegistry) applyAIMDFailureLocked(m *admissionMember, kind ErrorKind, now time.Time) {
	switch kind {
	case KindSoftRateLimit:
		m.limit = math.Max(1, math.Floor(m.limit*0.5))
		m.pausedUntil = now.Add(time.Second)
		a.emitLimitLocked(m, "rate_limited", now)
	case KindTimeout:
		m.limit = math.Max(1, math.Floor(m.limit*0.9))
		a.emitLimitLocked(m, "timeout", now)
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
		Member: m.memberLabel(), Detail: detail,
	})
}

func (m *admissionMember) memberLabel() string {
	// lane 级事件只需要稳定的 channel:model 投影。
	return m.label
}
