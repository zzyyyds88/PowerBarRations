package api

import (
	"net/http"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/internal/route"
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
	// 附运行态：explicit 车道给出"当前真正可选（未冷却且熔断非 open）"的成员数，
	// 让路由页的"可调用"反映健康而不是只看"车道存在"（routing-spec §7）。
	items := make([]gin.H, 0, len(summaries))
	for _, s := range summaries {
		item := gin.H{
			"model":                  s.Model,
			"source":                 s.Source,
			"routable":               s.Routable,
			"member_count":           s.MemberCount,
			"available_member_count": s.AvailableMemberCount,
		}
		if s.Source == model.RouteSourceExplicit {
			healthy, total := laneHealthCounts(s.Model)
			item["healthy_member_count"] = healthy
			item["health_member_count"] = total
			item["degraded"] = total > 0 && healthy == 0
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nil})
}

// laneHealthCounts 返回某车道"当前可选成员数 / 快照成员总数"。
// 解析失败（车道刚被删等竞态）按 (0,0) 处理，不因此让整个列表失败。
func laneHealthCounts(lane string) (healthy int, total int) {
	resolved, err := model.ResolveRoute(lane)
	if err != nil || resolved == nil || len(resolved.Members) == 0 {
		return 0, 0
	}
	snapshot := route.Default.For(lane).Health(resolved, route.CurrentCircuitSettings())
	total = len(snapshot.Members)
	for _, m := range snapshot.Members {
		if m.Available {
			healthy++
		}
	}
	return healthy, total
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
			"channel":         m.Channel,
			"channel_enabled": m.ChannelEnabled,
			"upstream_model":  m.UpstreamModel,
			"priority":        m.Priority,
		}
		// upstream_override 必须在有成员级显式改名时无条件回传（api-spec §4.4）：
		// 此前条件 `!= m.UpstreamModel` 恒假——只要显式名非空且 ≠ 路由键，
		// 解析后的 upstream_model 就等于它，字段从不出现；控制台车道编辑器
		// 读不到原值，重新保存时会把全部成员的显式上游真名清空（实测数据丢失）。
		if m.UpstreamOverride != "" {
			item["upstream_override"] = m.UpstreamOverride
		}
		if m.PublicAlias != "" {
			item["public_alias"] = m.PublicAlias
		}
		members = append(members, item)
	}
	// 已配车道的模型也要能看到"声明了该模型但不在成员链里"的候选渠道，
	// 否则新增渠道声明后只能删车道重建（ui-spec §6.3）。
	candidates := make([]gin.H, 0)
	if resolved.Source == model.RouteSourceExplicit {
		memberChannels := map[int]bool{}
		for _, m := range resolved.Members {
			memberChannels[m.ChannelId] = true
		}
		suggested, suggestedErr := model.SuggestedMembers(resolved.RouteKey)
		if suggestedErr != nil {
			writeAPIError(c, suggestedErr)
			return
		}
		for _, m := range suggested {
			if memberChannels[m.ChannelId] {
				continue
			}
			candidates = append(candidates, gin.H{
				"channel":        m.Channel,
				"upstream_model": m.UpstreamModel,
				"priority":       m.Priority,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"model":      resolved.Model,
		"source":     resolved.Source,
		"routable":   resolved.Source == model.RouteSourceExplicit,
		"mode":       resolved.Mode,
		"config":     resolved.Config,
		"members":    members,
		"candidates": candidates,
	})
}
