package api

import (
	"net/http"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 模型路由是观察面：渠道声明的 Models 会以 source=unconfigured、routable=false
// 出现在这里，但只有固化了同名启用车道（source=explicit）才真正可调用
// （ADR 0005、routing-spec §1.1、api-spec §5.7）。

// ListModels GET /api/v1/models
//
// next_cursor 固定为 null：本端点不分页（api-spec §2.4 约定耗尽/不分页时为
// null）。此前写空串，严格判空 `=== null` 的客户端会把它当成有效游标而反复
// 请求第一页；其余列表端点用 nextCursor 变量，输出本就是 null。
func ListModels(c *gin.Context) {
	summaries, err := model.ListModelSummaries()
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": summaries, "next_cursor": nil})
}

// GetRoute GET /api/v1/routes/{model}
//
// 已配车道 → 返回真实成员链；未配车道 → 返回"渠道声明"的建议成员链并标
// `routable=false`（ADR 0005）。模型面请求此时返回
// `503 No available channel for model <X>`（与"全挂"同形）。
func GetRoute(c *gin.Context) {
	modelName := strings.TrimPrefix(c.Param("model"), "/")
	resolved, err := model.ResolveRouteForDisplay(modelName)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	members := make([]gin.H, 0, len(resolved.Members))
	for _, m := range resolved.Members {
		item := gin.H{
			"channel":        m.Channel,
			"upstream_model": m.UpstreamModel,
			"priority":       m.Priority,
		}
		if m.UpstreamOverride != "" && m.UpstreamOverride != m.UpstreamModel {
			item["upstream_override"] = m.UpstreamOverride
		}
		if m.PublicAlias != "" {
			item["public_alias"] = m.PublicAlias
		}
		members = append(members, item)
	}
	c.JSON(http.StatusOK, gin.H{
		"model":    resolved.Model,
		"source":   resolved.Source,
		"routable": resolved.Source == model.RouteSourceExplicit,
		"mode":     resolved.Mode,
		"config":   resolved.Config,
		"members":  members,
	})
}
