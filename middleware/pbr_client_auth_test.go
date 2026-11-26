package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 限流器记账口径（token-spec §3.4 第 6 步）：
// 只有被放行的请求才允许写入 RPM 窗口与并发计数；被拒的请求不得占用配额。
func TestLimiterConcurrencyRejectDoesNotBurnRPM(t *testing.T) {
	l := &keyLimiter{}
	lease, _, ok := l.acquire(10, 1)
	require.True(t, ok)
	defer lease.Release()
	if lease, _, ok := l.acquire(10, 1); ok {
		lease.Release()
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
		lease, _, ok := l.acquire(3, 0)
		require.True(t, ok)
		defer lease.Release()
	}
	lease, wait, ok := l.acquire(3, 0)
	if ok {
		lease.Release()
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
	lease, _, ok := l.acquire(0, 1)
	require.True(t, ok)
	if lease, _, ok := l.acquire(0, 1); ok {
		lease.Release()
		t.Fatal("second request should be rejected while slot is held")
	}
	lease.Release()
	replacement, _, ok := l.acquire(0, 1)
	require.True(t, ok, "slot should be reusable after release")
	defer replacement.Release()

	lease.Release()
	lease.Release()
	if l.inflight != 1 {
		t.Fatalf("inflight=%d, want 1: repeated release affected another in-flight request", l.inflight)
	}
}

func TestLimiterDynamicRPMRetryAfterUsesNewLimitBoundary(t *testing.T) {
	l := &keyLimiter{}
	now := time.Now().Unix()
	l.recent = []int64{now - 50, now - 40, now - 30, now - 20, now - 10}

	lease, wait, ok := l.acquire(2, 0)
	if ok {
		lease.Release()
		t.Fatal("request should be rejected after RPM is dynamically lowered to 2")
	}
	// len(recent)-rpm = 3，因此应等待 now-20 那条滑出，即 40 秒；使用
	// recent[0] 会错误返回 10 秒。
	assert.Equal(t, 40, wait)
}

// rpm=0 表示不限：不应记录任何窗口数据，也不应因"窗口为空"而误判。
func TestLimiterZeroRPMIsUnlimited(t *testing.T) {
	l := &keyLimiter{}
	for i := 0; i < 100; i++ {
		lease, _, ok := l.acquire(0, 0)
		require.True(t, ok)
		lease.Release()
	}
	if len(l.recent) != 0 {
		t.Fatalf("recent=%d, want 0 when rpm is unlimited", len(l.recent))
	}
}

func TestKeyLeaseReleaseIsIdempotentAndPairPrecise(t *testing.T) {
	l := &keyLimiter{}
	first, _, ok := l.acquire(0, 2)
	require.True(t, ok)
	second, _, ok := l.acquire(0, 2)
	require.True(t, ok)

	first.Release()
	first.Release()
	assert.Equal(t, 1, l.inflight)

	third, _, ok := l.acquire(0, 2)
	require.True(t, ok)
	assert.Equal(t, 2, l.inflight)

	second.Release()
	assert.Equal(t, 1, l.inflight, "releasing first lease must not release second lease")
	third.Release()
	assert.Equal(t, 0, l.inflight)
}

func TestPBRClientKeyLeaseFrom(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.Nil(t, PBRClientKeyLeaseFrom(c))

	lease := &KeyLease{}
	c.Set(contextKeyPBRClientLease, lease)
	assert.Same(t, lease, PBRClientKeyLeaseFrom(c))
}

// 删除密钥后其限流条目必须消失，否则 map 随"建了删、删了再建"无限增长。
func TestDropPBRKeyLimiterEvictsEntry(t *testing.T) {
	limiter := limiterFor(987654)
	lease, _, ok := limiter.acquire(5, 0)
	require.True(t, ok)
	defer lease.Release()
	DropPBRKeyLimiter(987654)

	pbrLimitersMu.Lock()
	_, stillThere := pbrLimiters[987654]
	pbrLimitersMu.Unlock()
	if stillThere {
		t.Fatal("limiter entry survived key deletion: map grows unbounded")
	}

	fresh := limiterFor(987654)
	if len(fresh.recent) != 0 || fresh.inflight != 0 {
		t.Fatalf("recreated limiter not reset: recent=%d inflight=%d", len(fresh.recent), fresh.inflight)
	}
	DropPBRKeyLimiter(987654)
}

// token-spec §3.4：凭据只从 Authorization / X-Api-Key / x-goog-api-key 取，
// 查询串 ?key= 不再被当作凭据（否则凭据会随日志、Referer、浏览器历史泄漏）。
func TestModelFaceCredentialIgnoresQueryKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?key=query-secret", nil)
	if got := modelFaceCredential(c); got != "" {
		t.Fatalf("query key must not be accepted as credential, got %q", got)
	}

	c.Request.Header.Set("X-Api-Key", "header-secret")
	if got := modelFaceCredential(c); got != "header-secret" {
		t.Fatalf("X-Api-Key should still work, got %q", got)
	}
}

func TestKeyLeaseReleaseAfterContextCancellation(t *testing.T) {
	l := &keyLimiter{}
	lease, _, ok := l.acquire(0, 1)
	require.True(t, ok)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	select {
	case <-ctx.Done():
		lease.Release()
	default:
		t.Fatal("test context should be cancelled")
	}
	assert.Equal(t, 0, l.inflight)
}
