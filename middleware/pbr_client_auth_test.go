package middleware

import "testing"

// 限流器记账口径（token-spec §3.4 第 6 步）：
// 只有被放行的请求才允许写入 RPM 窗口与并发计数；被拒的请求不得占用配额。
//
// 回归背景：acquire 曾"先记 RPM、再查并发"，导致仅因并发超限被拒的请求
// 也会在 recent 里留下一条记录（吃掉 RPM 配额），且该路径提前返回不释放
// inflight。两个上限单独使用时都看不出来，必须同时设置才能复现。

func TestLimiterConcurrencyRejectDoesNotBurnRPM(t *testing.T) {
	l := &keyLimiter{}
	if _, ok := l.acquire(10, 1); !ok {
		t.Fatal("first request should pass")
	}
	if _, ok := l.acquire(10, 1); ok {
		t.Fatal("second request should be rejected by concurrency cap")
	}
	if len(l.recent) != 1 {
		t.Fatalf("recent=%d, want 1: concurrency-rejected request consumed RPM quota", len(l.recent))
	}
	if l.inflight != 1 {
		t.Fatalf("inflight=%d, want 1: rejected request leaked an inflight slot", l.inflight)
	}
}

func TestLimiterRPMRejectDoesNotConsumeQuota(t *testing.T) {
	l := &keyLimiter{}
	for i := 0; i < 3; i++ {
		if _, ok := l.acquire(3, 0); !ok {
			t.Fatalf("request %d should pass (rpm=3)", i+1)
		}
	}
	wait, ok := l.acquire(3, 0)
	if ok {
		t.Fatal("fourth request should be rejected by rpm cap")
	}
	if wait < 1 || wait > 60 {
		t.Fatalf("Retry-After=%d, want 1..60 seconds", wait)
	}
	if len(l.recent) != 3 {
		t.Fatalf("recent=%d, want 3: rejected request must not extend the window", len(l.recent))
	}
}

func TestLimiterReleaseFreesConcurrencySlot(t *testing.T) {
	l := &keyLimiter{}
	if _, ok := l.acquire(0, 1); !ok {
		t.Fatal("first request should pass")
	}
	if _, ok := l.acquire(0, 1); ok {
		t.Fatal("second request should be rejected while slot is held")
	}
	l.release()
	if _, ok := l.acquire(0, 1); !ok {
		t.Fatal("slot should be reusable after release")
	}
	// release 不允许把计数压到负数（重复 release 时并发上限会失效）
	l.release()
	l.release()
	if l.inflight != 0 {
		t.Fatalf("inflight=%d, want 0", l.inflight)
	}
}

// rpm=0 表示不限：不应记录任何窗口数据，也不应因"窗口为空"而误判。
func TestLimiterZeroRPMIsUnlimited(t *testing.T) {
	l := &keyLimiter{}
	for i := 0; i < 100; i++ {
		if _, ok := l.acquire(0, 0); !ok {
			t.Fatalf("request %d rejected while both caps are unlimited", i+1)
		}
	}
	if len(l.recent) != 0 {
		t.Fatalf("recent=%d, want 0 when rpm is unlimited", len(l.recent))
	}
}

// 删除密钥后其限流条目必须消失，否则 map 随"建了删、删了再建"无限增长。
func TestDropPBRKeyLimiterEvictsEntry(t *testing.T) {
	limiter := limiterFor(987654)
	if _, ok := limiter.acquire(5, 0); !ok {
		t.Fatal("acquire should pass")
	}
	DropPBRKeyLimiter(987654)

	pbrLimitersMu.Lock()
	_, stillThere := pbrLimiters[987654]
	pbrLimitersMu.Unlock()
	if stillThere {
		t.Fatal("limiter entry survived key deletion: map grows unbounded")
	}

	// 重建同 ID 的条目必须是全新的（窗口不残留）
	fresh := limiterFor(987654)
	if len(fresh.recent) != 0 || fresh.inflight != 0 {
		t.Fatalf("recreated limiter not reset: recent=%d inflight=%d", len(fresh.recent), fresh.inflight)
	}
	DropPBRKeyLimiter(987654)
}
