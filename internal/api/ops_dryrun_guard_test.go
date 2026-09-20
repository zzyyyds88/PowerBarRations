package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// dry-run 声明的**表内守卫**（docs/api-spec-v1.md §2.4、§5.9）。
//
// 全量守卫在 router/dryrun_coverage_test.go（遍历真实注册的全部管理面写路由）；
// 这里只保证 opsRoutes 表自身声明完整、且 reject 都有替代做法。
func TestEveryOpsWriteRouteDeclaresDryRun(t *testing.T) {
	for _, route := range opsRoutes() {
		if !isWriteMethod(route.Method) {
			continue
		}
		assert.Contains(t,
			[]DryRunMode{DryRunPreview, DryRunReject}, route.DryRun,
			"%s %s 未声明 dry-run 类别（api-spec §2.4）", route.Method, route.Path)
	}
}

// reject 必须给出替代做法，否则调用方只被告知"不行"，不知道该怎么办。
func TestOpsDryRunRejectRoutesExplainAlternatives(t *testing.T) {
	for _, route := range opsRoutes() {
		if route.DryRun == DryRunReject {
			assert.NotEmpty(t, route.DryRunReason,
				"%s %s 声明为 reject 但未给替代做法", route.Method, route.Path)
		}
	}
}

// 只读方法不参与 dry-run 声明（它们本就没有副作用）。
func TestOpsDryRunDeclarationsAreConsistent(t *testing.T) {
	for _, route := range opsRoutes() {
		if route.Method == http.MethodGet {
			assert.Empty(t, route.DryRun, "GET %s 不应声明 dry-run", route.Path)
			continue
		}
		assert.NotEmpty(t, route.DryRun, "写端点必须声明：%s %s", route.Method, route.Path)
	}
}
