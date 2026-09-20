package router

import (
	"sort"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/internal/api"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// design-v1 §16.3 验收断言：openapi.json 必须覆盖**所有已注册路由**。
//
// 回归背景：独立复审指出 openapi 是手写 map（非"以代码为源"），因此存在
// "新增路由漏登记"的漂移风险。此测试把该风险变成构建期失败：注册集合与
// 文档路径集合必须一一对应。
//
// 范围（重要）：openapi.json 的契约面是**管理 API**（`/api/*`），servers.url=/api。
// 根路径的文档页 `/doc`、`/doc/ui`、`/llms.txt`（api-spec §5.1）不是管理 API 端点，
// 刻意不登记进 openapi——它们由本测试显式豁免，见 docOnlyRoutes。
func TestOpenAPICoversEveryRegisteredRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetPBRRouter(engine)

	// 已注册路由 → OpenAPI path（:param → {param}，*model → {model}）。
	registered := map[string]bool{}
	for _, info := range engine.Routes() {
		if !strings.HasPrefix(info.Path, "/api/v1") {
			continue
		}
		path := strings.TrimPrefix(info.Path, "/api/v1")
		path = normalizeOpenAPIPath(path)
		registered[strings.ToLower(info.Method)+" "+path] = true
	}
	require.NotEmpty(t, registered)

	doc := api.OpenAPIPathsForTest()
	documented := map[string]bool{}
	for path, rawMethods := range doc {
		methods, ok := rawMethods.(gin.H)
		if !ok {
			continue
		}
		for method := range methods {
			documented[strings.ToLower(method)+" "+path] = true
		}
	}

	var missing, extra []string
	for route := range registered {
		if !documented[route] {
			missing = append(missing, route)
		}
	}
	for route := range documented {
		if !registered[route] {
			extra = append(extra, route)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	assert.Empty(t, missing, "以下已注册路由未登记进 OpenAPI：%v", missing)
	assert.Empty(t, extra, "OpenAPI 登记了并不存在的路由：%v", extra)
}

// TestDocRoutesAreOutsideOpenAPI 固化"文档页不进 openapi"的边界。
//
// api-spec §5.1 把 `/doc`、`/doc/ui`、`/llms.txt` 列为管理 API 的能力面，
// 但它们返回的是手册/UI 页面而不是可被工具解析的资源，且 openapi.json 的
// servers 只声明 /api。这里显式断言三者已注册、但**不在** openapi 里，
// 避免"悄悄登记进去"或"悄悄从路由消失"两种漂移。
func TestDocRoutesAreOutsideOpenAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetPBRRouter(engine)

	docRoutes := map[string]bool{"/doc": false, "/doc/ui": false, "/llms.txt": false}
	for _, info := range engine.Routes() {
		if _, ok := docRoutes[info.Path]; ok && info.Method == "GET" {
			docRoutes[info.Path] = true
		}
	}
	for path, found := range docRoutes {
		assert.True(t, found, "文档页路由未注册：GET %s", path)
	}

	doc := api.OpenAPIPathsForTest()
	for path := range docRoutes {
		_, exists := doc[path]
		assert.False(t, exists, "文档页 %s 不属于 /api 契约面，不得登记进 openapi.json", path)
	}
}

// normalizeOpenAPIPath 把 gin 的路径参数语法转成 OpenAPI 的花括号语法。
func normalizeOpenAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + part[1:] + "}"
		} else if strings.HasPrefix(part, "*") {
			parts[i] = "{" + part[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}
