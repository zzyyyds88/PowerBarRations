package route

import "time"

// DefaultProbeLeaseTTL 是探测租约的安全上界。探测请求可能已经提交长流式响应，
// 不能使用首事件超时作为租约到期时间；该上界只用于兜底回收泄漏 owner。
const DefaultProbeLeaseTTL = 30 * time.Minute

// ProbeLease 是恢复探测所有权的唯一凭证。释放必须同时匹配指针与 generation。
type ProbeLease struct {
	runtime    *Runtime
	key        MemberKey
	generation uint64
	acquiredAt int64
	expiresAt  int64
}

func (lease *ProbeLease) Key() MemberKey    { return lease.key }
func (lease *ProbeLease) AcquiredAt() int64 { return lease.acquiredAt }
func (lease *ProbeLease) ExpiresAt() int64  { return lease.expiresAt }

// AcquireProbe 取得未过期的单探测租约；ttl<=0 使用安全默认值。
func (runtime *Runtime) AcquireProbe(key MemberKey, ttl time.Duration) (*ProbeLease, bool) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.acquireProbeLocked(key, ttl)
}

func (runtime *Runtime) acquireProbeLocked(key MemberKey, ttl time.Duration) (*ProbeLease, bool) {
	if ttl <= 0 {
		ttl = DefaultProbeLeaseTTL
	}
	now := nowMs()
	runtime.expireProbeLocked(now)
	if runtime.probe != nil {
		return nil, false
	}
	lease := &ProbeLease{runtime: runtime, key: key, generation: runtime.generation,
		acquiredAt: now, expiresAt: now + ttl.Milliseconds()}
	runtime.probe = lease
	return lease, true
}

// ReleaseProbe 只释放与当前代次、指针和成员键精确匹配的租约。
func (runtime *Runtime) ReleaseProbe(lease *ProbeLease) bool {
	if runtime == nil || lease == nil || lease.runtime != runtime {
		return false
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.probe != lease || lease.generation != runtime.generation || lease.key != runtime.probe.key {
		return false
	}
	runtime.probe = nil
	return true
}

// ProbePending 只读判断当前是否存在匹配 key 的有效租约；到期不会在此修改运行态。
func (runtime *Runtime) ProbePending(key MemberKey, now int64) bool {
	if runtime == nil {
		return false
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.probe != nil && runtime.probe.key == key && runtime.probe.expiresAt > now
}

func (runtime *Runtime) expireProbeLocked(now int64) {
	if runtime.probe != nil && runtime.probe.expiresAt <= now {
		// 只失效租约本身；generation 只由 Reset 推进，避免一个到期探测
		// 把同车道其它在途请求的运行时记账全部判为旧代次。
		runtime.probe = nil
	}
}
