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
