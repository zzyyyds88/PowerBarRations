package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pbr/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 非模型面请求体上限：超过上限必须 413，上限内必须放行且体可读。
func TestAnonymousRequestBodyLimitRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := constant.AnonymousRequestBodyLimitKB
	constant.AnonymousRequestBodyLimitKB = 1 // 1KB，便于测试
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = original })

	engine := gin.New()
	engine.Use(AnonymousRequestBodyLimit())
	engine.POST("/api/setup", func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil || len(body) != 1024 {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
	})

	small := httptest.NewRecorder()
	engine.ServeHTTP(small, httptest.NewRequest(http.MethodPost, "/api/setup",
		strings.NewReader(strings.Repeat("a", 1024))))
	require.Equal(t, http.StatusOK, small.Code, "limit 以内的请求必须放行")

	large := httptest.NewRecorder()
	engine.ServeHTTP(large, httptest.NewRequest(http.MethodPost, "/api/setup",
		strings.NewReader(strings.Repeat("a", 4096))))
	require.Equal(t, http.StatusRequestEntityTooLarge, large.Code, "超过上限必须 413")
}
