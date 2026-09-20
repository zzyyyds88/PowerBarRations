package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// dry-run 声明的**构建期守卫**（docs/api-spec-v1.md §2.4、§5.9）。
//
// 背景：运维适配层复用基座 controller，而 controller 不读 dry_run，历史上导致
// 15 个破坏性写端点带 ?dry_run=true 时静默落库。本守卫把"必须声明"变成硬约束：
// 任何写方法（POST/PUT/DELETE）没有声明 DryRun 即失败，漏声明不可能悄悄退化成静默写入。
func TestEveryOpsWriteRouteDeclaresDryRun(t *testing.T) {
	for _, route := range opsRoutes() {
		switch route.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete:
			// 写方法必须声明，且必须是三种合法值之一。
			assert.Contains(t,
				[]DryRunMode{DryRunPreview, DryRunReject, DryRunIrrelevant}, route.DryRun,
				"%s %s 未声明 dry-run 类别（api-spec §2.4）", route.Method, route.Path)
		}
	}
}

// reject / irrelevant 必须给出理由，否则调用方无法判断"为什么不能预览"。
func TestOpsDryRunNonPreviewRoutesExplainWhy(t *testing.T) {
	for _, route := range opsRoutes() {
		if route.DryRun == DryRunReject || route.DryRun == DryRunIrrelevant {
			assert.NotEmpty(t, route.DryRunReason,
				"%s %s 声明为 %s 但未给理由", route.Method, route.Path, route.DryRun)
		}
	}
}

// preview 端点不得声明为 reject/irrelevant；只读方法不应被误标为 preview。
func TestOpsDryRunDeclarationsAreConsistent(t *testing.T) {
	for _, route := range opsRoutes() {
		if route.Method == http.MethodGet {
			assert.NotEqual(t, DryRunPreview, route.DryRun,
				"GET %s 是只读端点，不应声明为 preview", route.Path)
			continue
		}
		assert.NotEmpty(t, route.DryRun, "写端点必须声明：%s %s", route.Method, route.Path)
	}
}

// 声明为 preview 的端点必须真的挂上 dry-run 处理——由行为测试覆盖；
// 这里先固化"稳定面里不存在 reject 端点"这一现状（api-spec §5.9 未列 reject）。
func TestOpsRoutesHaveNoRejectDeclarations(t *testing.T) {
	for _, route := range opsRoutes() {
		assert.NotEqual(t, DryRunReject, route.DryRun,
			"%s %s 声明为 reject；若确需，请同步 api-spec §5.9", route.Method, route.Path)
	}
}
