package api

import (
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/zzyyyds88/PowerBarRations/internal/apierr"

	"github.com/gin-gonic/gin"
)

// dry-run 强制声明（docs/api-spec-v1.md §2.4、§5.9；design-v1 §5.1#4）。
//
// 背景：运维适配层（ops.go）与部分稳定面 handler 复用的基座 controller 全部不读
// dry_run，历史上导致大量破坏性写端点在 ?dry_run=true 时**静默落库**。
//
// **安全默认**：?dry_run=true 表示"我要求一个不产生副作用的预览"。因此每个
// 管理面写端点必须显式声明：
//
//   - preview ：返回 diff，**绝不落库**；
//   - reject  ：不支持预览 → 400 dry_run_not_supported，**绝不执行 handler**。
//
// **没有第三个类别**。"这个端点有副作用但我没实现预览"不是一种 dry-run 语义，
// 必须声明 reject，让调用方明确知道"要预览就得换端点/换做法"，而不是收到 200
// 却发现已经改库、发了请求、建了任务。只读端点（GET）不参与声明。
//
// 声明覆盖**全部**管理面写路由（稳定面 + 控制台内部面），不只是一张表：
// 守卫测试 TestEveryManagementWriteRouteDeclaresDryRun 遍历 engine.Routes()，
// 任何未声明的写路由都会让构建失败。
type DryRunMode string

const (
	// DryRunPreview：支持预览。带 ?dry_run=true 时返回 diff 且不落库。
	DryRunPreview DryRunMode = "preview"
	// DryRunReject：不支持预览。带 ?dry_run=true 时 400 且**不执行**。
	DryRunReject DryRunMode = "reject"
)

// dryRunRejectMessage 是 reject 端点的固定文案（调用方按 code 分支，不解析 message）。
const dryRunRejectMessage = "this endpoint does not support dry-run"

// isWriteMethod 判断是否为可能产生副作用的 HTTP 方法。
func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

var (
	dryRunMu       sync.RWMutex
	dryRunRegistry = map[string]DryRunMode{}
	dryRunHints    = map[string]string{}
)

// RegisterDryRun 登记某路由的 dry-run 类别（path 用 gin 的 c.FullPath() 形式）。
func RegisterDryRun(method, path string, mode DryRunMode, hint string) {
	dryRunMu.Lock()
	defer dryRunMu.Unlock()
	key := dryRunKey(method, path)
	dryRunRegistry[key] = mode
	if hint != "" {
		dryRunHints[key] = hint
	}
}

// dryRunKey 归一化管理面前缀，使 /api/x 与 /api/v1/x 共用一份登记。
func dryRunKey(method, path string) string {
	for _, prefix := range []string{"/api/v1", "/api"} {
		if path == prefix {
			path = "/"
			break
		}
		if strings.HasPrefix(path, prefix+"/") {
			path = strings.TrimPrefix(path, prefix)
			break
		}
	}
	return strings.ToUpper(method) + " " + path
}

// DryRunDeclaration 返回某路由的声明（供守卫测试与中间件使用）。
func DryRunDeclaration(method, path string) (DryRunMode, string, bool) {
	dryRunMu.RLock()
	defer dryRunMu.RUnlock()
	key := dryRunKey(method, path)
	mode, ok := dryRunRegistry[key]
	return mode, dryRunHints[key], ok
}

// DryRunKeyForTest 暴露归一化键给守卫测试（与登记时同一口径）。
func DryRunKeyForTest(method, path string) string { return dryRunKey(method, path) }

// DryRunDeclaredRoutes 返回全部已声明的 "METHOD path"（供守卫测试）。
func DryRunDeclaredRoutes() []string {
	dryRunMu.RLock()
	defer dryRunMu.RUnlock()
	out := make([]string, 0, len(dryRunRegistry))
	for k := range dryRunRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ResetDryRunRegistryForTest 清空登记（仅测试用）。
func ResetDryRunRegistryForTest() {
	dryRunMu.Lock()
	defer dryRunMu.Unlock()
	dryRunRegistry = map[string]DryRunMode{}
	dryRunHints = map[string]string{}
}

// DryRunMiddleware 是管理面的 dry-run 安全网：
// 写方法 + ?dry_run=true + 未声明为 preview ⇒ 400 且不执行 handler。
//
// 它挂在**整个管理面**上（不只运维表），因此"忘记实现预览"只会变成"明确拒绝"，
// 不可能退化成"静默写入"。
func DryRunMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isWriteMethod(c.Request.Method) || !dryRun(c) {
			c.Next()
			return
		}
		// FullPath 在路由匹配后才可用；此处已在 handler 链上，故可读。
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		mode, hint, ok := DryRunDeclaration(c.Request.Method, path)
		if ok && mode == DryRunPreview {
			// 由 handler 自行返回 diff（不落库）。
			c.Next()
			return
		}
		if hint == "" {
			hint = "remove ?dry_run=true to execute, or use the endpoint's dedicated preview API"
		}
		apierr.Write(c, http.StatusBadRequest, apierr.CodeDryRunNotSupported,
			dryRunRejectMessage, hint)
		c.Abort()
	}
}

// previewOps 是 preview 端点的统一响应：写一条 dry_run=true 审计并返回契约形状。
func previewOps(c *gin.Context, resource, action, name string) {
	previewOpsDiff(c, resource, action, name, nil)
}

// previewOpsDiff 同上，但允许调用方提供额外的结构化预览（如将删文件名清单）。
func previewOpsDiff(c *gin.Context, resource, action, name string, extra gin.H) {
	writeAudit(c, action, resource, name)
	diff := gin.H{"add": []string{}, "update": []string{}, "remove": []string{}}
	switch action {
	case "add":
		diff["add"] = []string{name}
	case "update":
		diff["update"] = []string{name}
	case "remove":
		diff["remove"] = []string{name}
	}
	payload := gin.H{"dry_run": true, "valid": true, "diff": gin.H{resource: diff}}
	for k, v := range extra {
		payload[k] = v
	}
	c.JSON(http.StatusOK, payload)
}

// previewOpsNames 返回"将变更的名字列表"式预览，用于批量端点。
func previewOpsNames(c *gin.Context, resource, action string, add, update, remove []string, extra gin.H) {
	all := append(append(append([]string{}, add...), update...), remove...)
	writeAudit(c, action, resource, strings.Join(all, ","))
	payload := gin.H{
		"dry_run": true,
		"valid":   true,
		"diff": gin.H{resource: gin.H{
			"add":    nonNilStrings(add),
			"update": nonNilStrings(update),
			"remove": nonNilStrings(remove),
		}},
	}
	for k, v := range extra {
		payload[k] = v
	}
	c.JSON(http.StatusOK, payload)
}

// nonNilStrings 保证契约里数组恒为 []（不是 null）。
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
