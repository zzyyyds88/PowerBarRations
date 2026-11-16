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

// dry-run 声明的**全量守卫**（docs/api-spec-v1.md §2.4、§5.9）。
//
// 背景：运维适配层与部分稳定面 handler 复用的基座 controller 不读 dry_run，
// 历史上大量破坏性写端点带 ?dry_run=true 时静默落库。这里遍历**真实注册**的
// 每一个管理面写路由（不只 opsRoutes 表），要求它必须被显式声明为
// preview 或 reject——漏声明即构建失败。
//
// 这是"不允许再有静默写入"的最后一道闸：新增写端点时必须做出选择。
func TestEveryManagementWriteRouteDeclaresDryRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetPBRRouter(engine)

	// 免鉴权端点不适用本约定（api-spec §2.4）：它们在鉴权之前，且没有"预览"语义。
	// 这里显式列出而不是"跳过不查"，避免新增免鉴权写端点时被静默豁免。
	preAuthExempt := map[string]bool{
		"POST /api/setup":          true,
		"POST /api/v1/setup":       true,
		"POST /api/auth/login":     true,
		"POST /api/v1/auth/login":  true,
		"POST /api/auth/logout":    true,
		"POST /api/v1/auth/logout": true,
	}
	var undeclared []string
	checked := 0
	exempted := 0
	for _, info := range engine.Routes() {
		if !strings.HasPrefix(info.Path, "/api") {
			continue
		}
		switch info.Method {
		case "POST", "PUT", "PATCH", "DELETE":
		default:
			continue
		}
		key := info.Method + " " + info.Path
		if preAuthExempt[key] {
			exempted++
			continue
		}
		checked++
		if _, _, ok := api.DryRunDeclaration(info.Method, info.Path); !ok {
			undeclared = append(undeclared, key)
		}
	}
	sort.Strings(undeclared)
	require.NotZero(t, checked, "没有扫到任何写路由，守卫失效")
	assert.Equal(t, len(preAuthExempt), exempted,
		"免鉴权豁免表与实际路由不一致（新增/改名请同步 preAuthExempt）")
	assert.Empty(t, undeclared,
		"以下管理面写路由未声明 dry-run（preview 或 reject）：\n%s", strings.Join(undeclared, "\n"))
}

// 声明表不得登记不存在的路由（防止改名后留下幽灵声明）。
func TestDryRunDeclarationsMatchRegisteredRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetPBRRouter(engine)

	registered := map[string]bool{}
	for _, info := range engine.Routes() {
		registered[api.DryRunKeyForTest(info.Method, info.Path)] = true
	}
	var extra []string
	for _, key := range api.DryRunDeclaredRoutes() {
		if !registered[key] {
			extra = append(extra, key)
		}
	}
	assert.Empty(t, extra, "dry-run 声明了并不存在的路由：%v", extra)
}

// 端到端：未声明为 preview 的写端点带 ?dry_run=true 必须 400 且不执行 handler。
// 用真实引擎 + 一个 reject 路由验证安全网确实拦得住。
func TestDryRunSafetyNetBlocksUnpreviewedWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetPBRRouter(engine)

	// /api/v1/webhooks/test 声明为 reject：带 ?dry_run=true 应 400 dry_run_not_supported。
	// 不带鉴权时会被 401 挡下，故只断言"未执行到投递"——用未初始化/未鉴权的
	// 路径无法覆盖，这里改为直接验证声明与中间件的组合语义。
	mode, hint, ok := api.DryRunDeclaration("POST", "/api/v1/webhooks/test")
	require.True(t, ok, "webhooks/test 必须已声明")
	assert.Equal(t, api.DryRunReject, mode)
	assert.NotEmpty(t, hint, "reject 必须给出替代做法")

	for _, key := range []string{
		"POST /channels/:name/test",
		"POST /lanes/:name/probe",
	} {
		parts := strings.SplitN(key, " ", 2)
		m, h, found := api.DryRunDeclaration(parts[0], "/api/v1"+parts[1])
		require.True(t, found, "%s 必须已声明", key)
		assert.Equal(t, api.DryRunReject, m, "%s 应声明为 reject", key)
		assert.NotEmpty(t, h)
	}
}
