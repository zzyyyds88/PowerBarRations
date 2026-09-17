package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"pbr/middleware"
	"pbr/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 退避必须按来源隔离，且命中退避时返回 429 + Retry-After，而不是 sleep 后放行。
func TestLoginBackoffIsPerSourceAndReturns429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PBRAdminCredential{}))
	t.Setenv(middleware.PBREnvAdminKey, "")
	t.Setenv(middleware.PBREnvAdminKeys, "")
	_, err := model.SetPBRAdminPassword("Correct-Pass-2026")
	require.NoError(t, err)

	const blockedSource = "203.0.113.10"
	t.Cleanup(func() {
		loginBackoff.succeed(blockedSource)
		loginBackoff.succeed("203.0.113.11")
	})

	login := func(ip, password string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"password":"`+password+`"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.RemoteAddr = ip + ":1234"
		Login(c)
		return recorder
	}

	// 第一次失败：401，并登记该来源。
	first := login(blockedSource, "wrong")
	require.Equal(t, http.StatusUnauthorized, first.Code)

	// 放大退避窗口，避免测试机偶发卡顿导致窗口过期（仍测同一来源）。
	for i := 0; i < 6; i++ {
		loginBackoff.fail(blockedSource)
	}

	// 同一来源立刻再试（即使口令正确）：429 + Retry-After，不再 sleep 后放行。
	second := login(blockedSource, "Correct-Pass-2026")
	require.Equal(t, http.StatusTooManyRequests, second.Code)
	retryAfter := strings.TrimSpace(second.Header().Get("Retry-After"))
	require.NotEmpty(t, retryAfter)
	seconds, convErr := strconv.Atoi(retryAfter)
	require.NoError(t, convErr)
	assert.GreaterOrEqual(t, seconds, 1)

	// 另一个来源不被牵连：正常登录成功。
	other := login("203.0.113.11", "Correct-Pass-2026")
	require.Equal(t, http.StatusOK, other.Code)
	assert.Contains(t, other.Body.String(), "admin_key")
}

// 退避状态机：指数增长、封顶、成功即清、来源隔离。
func TestLoginBackoffStateExponentialAndReset(t *testing.T) {
	var state loginBackoffState
	const source = "198.51.100.7"

	assert.Zero(t, state.retryAfter(source), "初始不应退避")
	state.fail(source)
	first := state.retryAfter(source)
	require.Greater(t, first, time.Duration(0))
	require.LessOrEqual(t, first, maxLoginDelay)

	assert.Zero(t, state.retryAfter("198.51.100.8"), "其它来源不得被牵连")

	for i := 0; i < 10; i++ {
		state.fail(source)
	}
	require.Greater(t, state.retryAfter(source), first, "连续失败应指数增长")
	require.LessOrEqual(t, state.retryAfter(source), maxLoginDelay, "退避不得超过上限")

	state.succeed(source)
	assert.Zero(t, state.retryAfter(source), "成功后必须清零")

	// 过期窗口应自动释放。
	state.fail("198.51.100.9")
	state.mu.Lock()
	state.entries["198.51.100.9"].blockedUntil = time.Now().Add(-time.Second)
	state.mu.Unlock()
	assert.Zero(t, state.retryAfter("198.51.100.9"), "已过窗口的来源必须可再次尝试")
}
