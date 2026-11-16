package api

import (
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/internal/apiresp"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 守卫：每条运维端点都必须声明响应策略（design-v1 §16.3）。
//
// 允许三种声明：自定义成功体、通用失败映射、或显式 Passthrough（SSE）。
// 三者皆无 = 漏声明，构建期失败。
func TestEveryOpsRouteDeclaresResponsePolicy(t *testing.T) {
	for _, route := range opsRoutes() {
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			p := route.Policy
			declared := p.Success != nil || len(p.Failures) > 0 || p.Passthrough
			assert.True(t, declared, "运维端点必须声明 ResponsePolicy：%s %s", route.Method, route.Path)
			assert.NotNil(t, route.Handler, "运维端点必须有 handler")
		})
	}
}

// 守卫：声明表与注册表必须一致——注册的运维路由都在表里，表里的都注册了。
//
// 通过把 gin 引擎的注册集合与 opsRoutes() 对比实现；任何"绕过表注册"或
// "表里有但没注册"都会失败。
func TestOpsRoutesMatchRegisteredRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	RegisterOpsRoutes(group)

	registered := map[string]bool{}
	for _, info := range engine.Routes() {
		registered[info.Method+" "+strings.TrimPrefix(info.Path, "/api/v1")] = true
	}

	declared := map[string]bool{}
	for _, route := range opsRoutes() {
		declared[route.Method+" "+route.Path] = true
	}

	require.NotEmpty(t, declared)
	assert.Equal(t, declared, registered, "运维路由声明表与注册表必须一一对应")
}

// 守卫：注册即登记响应策略（策略不可能被漏登记）。
func TestRegisterOpsRoutesRegistersPolicies(t *testing.T) {
	apiresp.ResetForTest()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterOpsRoutes(engine.Group("/api/v1"))

	for _, route := range opsRoutes() {
		_, ok := apiresp.Lookup(route.Method, "/api/v1"+route.Path)
		assert.True(t, ok, "策略未登记：%s %s", route.Method, route.Path)
	}
}

// 策略登记对 /api 与 /api/v1 两个前缀等价（前缀归一化）。
func TestPolicyLookupNormalizesPrefix(t *testing.T) {
	apiresp.ResetForTest()
	apiresp.Register("POST", "/api/v1/channels/batch/status", apiresp.Policy{Passthrough: true})

	for _, path := range []string{"/api/channels/batch/status", "/api/v1/channels/batch/status"} {
		_, ok := apiresp.Lookup("POST", path)
		assert.True(t, ok, "前缀归一化失败：%s", path)
	}
}
