package router

import (
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/internal/api"
	"github.com/zzyyyds88/PowerBarRations/internal/apiresp"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// 守卫：每个已注册的管理面路由都必须能被响应信封覆盖（design-v1 §16.3）。
//
// 覆盖有三种来源：
//  1. 稳定面 opsRoutes 显式登记的策略；
//  2. 基座面显式登记的策略（apiresp.Register）；
//  3. 二者都没有时回落 apiresp.Default —— 这仍然是"已覆盖"（成功裸化、失败按
//     code/状态归类），因此本测试断言的是"中间件确实会改写它"，而不是"必须显式登记"。
//
// 真正会失败的是：**显式登记了却不存在的路由**（登记漂移），以及策略被显式清空
// （既无 Success/Failures 也无 Passthrough 的显式登记等于漏声明）。
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
		// 规范化成 apiresp 的键（去掉 /api 或 /api/v1 前缀）。
		registered[strings.ToUpper(info.Method)+" "+normalizeEnvelopePath(info.Path)] = true
	}
	assert.NotEmpty(t, registered)

	// 1) 显式登记的稳定面策略必须对应真实注册的路由（不登记不存在的路由）。
	for _, route := range api.OpsRoutesForTest() {
		key := strings.ToUpper(route.Method) + " " + route.Path
		assert.True(t, registered[key], "运维策略登记了不存在的路由：%s", key)
		declared := route.Policy.Success != nil || len(route.Policy.Failures) > 0 || route.Policy.Passthrough
		assert.True(t, declared, "运维端点漏声明响应策略：%s", key)
	}

	// 2) 基座面显式登记的策略同样不得漂移。
	for _, key := range apiresp.RegisteredRoutes() {
		parts := strings.SplitN(key, " ", 2)
		assert.Len(t, parts, 2)
		assert.True(t, registered[key], "基座面策略登记了不存在的路由：%s", key)
	}
}

// 守卫：稳定面运维路由必须与注册表一一对应（既无漏注册，也无幽灵声明）。
func TestOpsRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	api.RegisterOpsRoutes(group)

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
