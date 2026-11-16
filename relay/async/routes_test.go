package async

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/v1"))

	registered := make(map[string]bool)
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{
		"GET /v1/files",
		"POST /v1/files",
		"GET /v1/files/:id",
		"DELETE /v1/files/:id",
		"GET /v1/files/:id/content",
		"POST /v1/batches",
		"GET /v1/batches",
		"GET /v1/batches/:id",
		"POST /v1/batches/:id/cancel",
	} {
		require.True(t, registered[want], "missing route %s", want)
	}
}
