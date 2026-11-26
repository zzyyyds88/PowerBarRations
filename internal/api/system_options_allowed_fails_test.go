package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// allowed_fails 的键是错误类型枚举：未知键必须在写库前拒绝，不能静默落库。
func TestPutSystemOptionsRejectsUnknownAllowedFailKind(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/v1/system/options",
		strings.NewReader(`{"circuit_allowed_fails":{"not_a_kind":3}}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")

	PutSystemOptions(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "unknown error kind")
}

func TestPutSystemOptionsRejectsNonPositiveAllowedFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/v1/system/options",
		strings.NewReader(`{"circuit_allowed_fails":{"hard_auth":0}}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")

	PutSystemOptions(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "hard_auth must be")
}

// 连续拒绝阈值/冷却时长是写入口契约：越界必须在落库前拒绝。
func TestPutSystemOptionsRejectsOutOfRangeRejectCooldownSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		body string
		want string
	}{
		{"threshold zero", `{"reject_streak_threshold":0}`, "reject_streak_threshold must be in [1, 1000]"},
		{"threshold too large", `{"reject_streak_threshold":1001}`, "reject_streak_threshold must be in [1, 1000]"},
		{"cooldown zero", `{"reject_cooldown_seconds":0}`, "reject_cooldown_seconds must be in [1, 86400]"},
		{"cooldown too large", `{"reject_cooldown_seconds":86401}`, "reject_cooldown_seconds must be in [1, 86400]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(
				http.MethodPut,
				"/api/v1/system/options",
				strings.NewReader(tc.body),
			)
			c.Request.Header.Set("Content-Type", "application/json")

			PutSystemOptions(c)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), tc.want)
		})
	}
}

// 熔断时长/滚动样本同样有硬上界：不设上界时指数退避会溢出。
func TestPutSystemOptionsRejectsOutOfRangeCircuitSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		body string
		want string
	}{
		{"open too large", `{"circuit_open_seconds":604801}`, "circuit_open_seconds must be in [0, 604800]"},
		{"max open too large", `{"circuit_max_open_seconds":604801}`, "circuit_max_open_seconds must be in [1, 604800]"},
		{"samples too large", `{"circuit_rolling_min_samples":21}`, "circuit_rolling_min_samples must be in [1, 20]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(
				http.MethodPut,
				"/api/v1/system/options",
				strings.NewReader(tc.body),
			)
			c.Request.Header.Set("Content-Type", "application/json")

			PutSystemOptions(c)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), tc.want)
		})
	}
}
