package router

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/controller"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// W7-A（design-v1 §10.2.1）：渠道路由表不再携带按角色的权限字段——PBR 只有一把
// 全量权限的管理密钥，整组鉴权是 middleware.PBRAuth()。这里锁住"路由仍在表里、
// handler 没被换掉"，以及"未带管理密钥一律拦下"。

func TestChannelRoutesKeepTheirHandlers(t *testing.T) {
	assertChannelRoute(t, http.MethodGet, "/default_base_urls", controller.GetChannelDefaultBaseURLs)
	assertChannelRoute(t, http.MethodPost, "/:id/status", controller.UpdateChannelStatus)
	assertChannelRoute(t, http.MethodPost, "/status/batch", controller.BatchUpdateChannelStatus)
	assertChannelRoute(t, http.MethodPut, "/", controller.UpdateChannel)
	assertChannelRoute(t, http.MethodDelete, "/:id", controller.DeleteChannel)
	assertChannelRoute(t, http.MethodPost, "/batch", controller.DeleteChannelBatch)
	assertChannelRoute(t, http.MethodDelete, "/disabled", controller.DeleteDisabledChannel)
	assertChannelRoute(t, http.MethodPut, "/tag", controller.EditTagChannels)
	assertChannelRoute(t, http.MethodPost, "/batch/tag", controller.BatchSetChannelTag)
}

func TestChannelRoutesStatusRoutesRegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api")

	require.NotPanics(t, func() {
		registerChannelRoutes(api)
	})
}

func TestChannelRoutesRejectMissingAdminKey(t *testing.T) {
	// 用环境变量指定管理密钥，使 PBRAuth 不依赖数据库就能判定"已初始化"。
	t.Setenv("PBR_ADMIN_KEY", "w7-router-test-admin-key")

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerChannelRoutes(engine.Group("/api"))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/channel/default_base_urls", nil))

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"error"`)
}

func assertChannelRoute(t *testing.T, method string, path string, handler any) {
	t.Helper()
	for _, route := range channelRoutes {
		if route.method == method && route.path == path {
			assert.Equal(t, reflect.ValueOf(handler).Pointer(), reflect.ValueOf(route.handler).Pointer())
			return
		}
	}
	t.Fatalf("route %s %s not found", method, path)
}
