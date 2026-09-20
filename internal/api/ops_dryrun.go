package api

import (
	"net/http"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/internal/apierr"

	"github.com/gin-gonic/gin"
)

// dry-run 强制声明（docs/api-spec-v1.md §2.4、§5.9；design-v1 §5.1#4）。
//
// 背景：运维适配层（ops.go）复用的基座 controller 全部不读 dry_run，历史上导致
// 15 个破坏性写端点在 ?dry_run=true 时**静默落库**。这里把"是否支持 dry-run"
// 变成路由声明的一部分，并在注册时挂上强制中间件：
//
//   - reject     ：带 ?dry_run=true 直接 400 dry_run_not_supported，**绝不执行 handler**；
//   - preview    ：由 handler 自行返回 diff（辅助函数 previewOps 统一形状）；
//   - irrelevant ：非配置类（只读/运行态/上游动作/任务触发），忽略参数照常执行。
//
// 守卫测试 TestEveryOpsWriteRouteDeclaresDryRun 要求每个写方法都必须声明，
// 使"漏声明"不可能悄悄退化成"静默写入"。
type DryRunMode string

const (
	// DryRunPreview：支持预览。带 ?dry_run=true 时返回 diff 且不落库。
	DryRunPreview DryRunMode = "preview"
	// DryRunReject：不支持预览。带 ?dry_run=true 时 400 且不执行。
	DryRunReject DryRunMode = "reject"
	// DryRunIrrelevant：非配置类端点，dry_run 无意义（只读/运行态/上游动作/任务触发）。
	DryRunIrrelevant DryRunMode = "irrelevant"
)

// dryRunRejectMessage 是 reject 端点的固定文案（调用方按 code 分支，不解析 message）。
const dryRunRejectMessage = "this endpoint does not support dry-run"

// dryRunRejectMiddleware 在 handler 之前拦下 reject 端点的 ?dry_run=true 请求。
//
// 它**必须**在 handler 之前 abort，否则就退回成"静默写入"。
func dryRunRejectMiddleware(hint string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !dryRun(c) {
			c.Next()
			return
		}
		apierr.Write(c, http.StatusBadRequest, apierr.CodeDryRunNotSupported, dryRunRejectMessage, hint)
		c.Abort()
	}
}

// previewOps 是 preview 端点的统一响应：写一条 dry_run=true 审计并返回契约形状。
//
// diff 的键按资源命名（api-spec §2.4）；动作类可用语义化最小对象。
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

// previewOpsNames 返回"将变更的名字列表"式预览，用于批量端点：
// diff.<resource>.{add,update,remove} 由调用方给出的分类列表填充。
func previewOpsNames(c *gin.Context, resource, action string, add, update, remove []string, extra gin.H) {
	writeAudit(c, action, resource, strings.Join(append(append(append([]string{}, add...), update...), remove...), ","))
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
