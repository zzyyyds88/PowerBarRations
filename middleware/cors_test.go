package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 通配来源下绝不能再允许携带凭据：* + Access-Control-Allow-Credentials: true
// 会被浏览器拒绝，且等于把管理面会话 Cookie 暴露给任意站点。
func TestCORSDoesNotAllowCredentialsWithWildcardOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(CORS())
	engine.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "https://evil.example")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	require.Equal(t, "*", recorder.Header().Get("Access-Control-Allow-Origin"))
	require.Empty(t, recorder.Header().Get("Access-Control-Allow-Credentials"),
		"AllowAllOrigins 下绝不能再允许凭据")
}

func TestVersionHeaderIsPBRBranded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Version())
	engine.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/x", nil))

	require.NotEmpty(t, recorder.Header().Get("X-PBR-Version"))
	require.Empty(t, recorder.Header().Get("X-New-Api-Version"))
}
