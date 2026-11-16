package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 访问日志不得带查询串：?key=/?token= 这类凭据会随日志长期留存。
func TestSetUpLoggerStripsQueryString(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var buf bytes.Buffer
	oldWriter := gin.DefaultWriter
	gin.DefaultWriter = &buf
	t.Cleanup(func() { gin.DefaultWriter = oldWriter })

	engine := gin.New()
	SetUpLogger(engine)
	engine.GET("/v1/models", func(c *gin.Context) { c.Status(http.StatusOK) })

	engine.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/v1/models?key=super-secret&api_key=another", nil))

	out := buf.String()
	if strings.Contains(out, "super-secret") || strings.Contains(out, "api_key") {
		t.Fatalf("access log leaked query credentials: %s", out)
	}
	if !strings.Contains(out, "/v1/models") {
		t.Fatalf("access log missing path: %s", out)
	}
}
