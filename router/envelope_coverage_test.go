package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/internal/api"
	"github.com/zzyyyds88/PowerBarRations/internal/apiresp"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// 守卫：**每一个**已注册的管理面路由都必须被响应信封覆盖。
//
// 这是 design-v1 §16.3「响应契约以表为源」的可执行断言：中间件对已登记路由用其策略、
// 对未登记路由回落 apiresp.Default，因此不存在"漏信封"的路由；本测试进一步保证
// **登记不漂移**（登记了不存在的路由即失败）。
func TestEveryManagementRouteIsEnvelopeCovered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	SetPBRRouter(engine)

	registered := map[string]bool{}
	for _, info := range engine.Routes() {
		if !strings.HasPrefix(info.Path, "/api") {
			continue
		}
		registered[strings.ToUpper(info.Method)+" "+normalizeEnvelopePath(info.Path)] = true
	}
	assert.NotEmpty(t, registered)

	// 稳定面声明表：不得登记不存在的路由，不得漏声明策略。
	for _, route := range api.OpsRoutesForTest() {
		key := strings.ToUpper(route.Method) + " " + route.Path
		assert.True(t, registered[key], "运维策略登记了不存在的路由：%s", key)
		declared := route.Policy.Success != nil || len(route.Policy.Failures) > 0 || route.Policy.Passthrough
		assert.True(t, declared, "运维端点漏声明响应策略：%s", key)
	}

	// 基座面显式登记：不得漂移。
	for _, key := range apiresp.RegisteredRoutes() {
		assert.True(t, registered[key], "基座面策略登记了不存在的路由：%s", key)
	}
}

// 守卫：运维端点（稳定面）必须与注册表一一对应。
func TestOpsRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api.RegisterOpsRoutes(engine.Group("/api/v1"))

	registered := map[string]bool{}
	for _, info := range engine.Routes() {
		registered[info.Method+" "+strings.TrimPrefix(info.Path, "/api/v1")] = true
	}
	declared := map[string]bool{}
	for _, route := range api.OpsRoutesForTest() {
		declared[route.Method+" "+route.Path] = true
	}
	assert.Equal(t, declared, registered, "运维路由声明表与注册表必须一一对应")
}

// 端到端：中间件确实把基座信封改写成契约形态（成功裸化、失败带状态码）。
func TestEnvelopeMiddlewareRewritesBaseEnvelope(t *testing.T) {
	apiresp.ResetForTest()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	group.Use(apiresp.Middleware())
	group.POST("/base-success", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"ok": 1}})
	})
	group.POST("/base-failure", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "参数错误"})
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/base-success", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"ok":1}`, rec.Body.String())

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/base-failure", nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code, "业务失败必须带真实状态码")
	assert.Contains(t, rec.Body.String(), `"code":"validation_failed"`)
}

// normalizeEnvelopePath 去掉管理面前缀，与 apiresp 的归一化口径一致。
func normalizeEnvelopePath(path string) string {
	for _, prefix := range []string{"/api/v1", "/api"} {
		if path == prefix {
			return "/"
		}
		if strings.HasPrefix(path, prefix+"/") {
			return strings.TrimPrefix(path, prefix)
		}
	}
	return path
}
