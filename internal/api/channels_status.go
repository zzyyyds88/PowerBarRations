package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 渠道状态（api-spec §6.12）：两个**只读**端点，供控制台渠道页的「健康」抽屉与列表徽章。
//
// 数据源三条，口径各自固定、互不越界：
//   - 运行态：进程内 route 注册表（重启清空，与车道健康快照同源同义）；车道从未服务过时
//     运行态为"冷启"（circuit=closed、无冷却、rolling_success_rate=0）；
//   - 窗口统计与逐日：小时聚合表（对 POST /logs/prune 免疫）；
//   - 平均耗时：明细日志（会被 prune 影响——api-spec §6.12 已明示，这是刻意取舍）；
//   - 探活历史：pbr_probe_logs（每次渠道探活落一条）；
//   - 上游模型变动：渠道设置里**上次探测的快照**（本端点不访问上游、无副作用）。

const (
	channelStatusWindow24h   = "24h"
	channelStatusWindow7d    = "7d"
	channelStatusRecentLimit = 20
)

// channelStatusMember 渠道在**某条车道**里的成员运行态：车道 + 成员健康快照字段。
// 嵌 route.MemberHealth 保证字段与车道健康快照（api-spec §6.5）逐字段同义，不另造口径。
type channelStatusMember struct {
	Lane string `json:"lane"`
	route.MemberHealth
}

// channelStatusLaneRef 该渠道在某条车道里的成员引用（配置态，来自车道成员链）。
type channelStatusLaneRef struct {
	Lane          string `json:"lane"`
	Member        string `json:"member"`
	Model         string `json:"model"`
	UpstreamModel string `json:"upstream_model"`
	Priority      int    `json:"priority"`
	Enabled       bool   `json:"enabled"`
}

// channelStatusSummary 窗口内的统计汇总（口径见 api-spec §6.12）。
type channelStatusSummary struct {
	Requests      int64   `json:"requests"`
	Successes     int64   `json:"successes"`
	Failures      int64   `json:"failures"`
	SuccessRate   float64 `json:"success_rate"`
	AvgTotalMs    float64 `json:"avg_total_ms"`
	AvgTTFTMs     float64 `json:"avg_ttft_ms"`
	EstimatedCost float64 `json:"estimated_cost"`
}

// parseChannelStatusWindow 解析 ?window=（只接受 24h / 7d，其余 422）。
func parseChannelStatusWindow(c *gin.Context) (label string, seconds int64, ok bool) {
	label = strings.ToLower(strings.TrimSpace(c.DefaultQuery("window", channelStatusWindow24h)))
	switch label {
	case channelStatusWindow24h:
		return label, 24 * 3600, true
	case channelStatusWindow7d:
		return label, 7 * 24 * 3600, true
	default:
		apierr.Validation(c, "window must be 24h or 7d")
		return "", 0, false
	}
}

// channelStatusWindowBounds 窗口的 [from, until) 边界（秒）。
//
// `from` **按小时下取整对齐**。聚合表只有小时桶，窗口总数只能由小时桶相加得出：
// 起点若落在桶中间，聚合读的"桶与窗口相交"语义会把该桶整份计入，把窗口悄悄拉长
// 最多一小时（实测 24h 档因此把前一天整天的量算了进来，渠道请求数虚高近一倍）。
// 对齐到桶边界后，"最近 N 小时"成为可精确复算的口径，且与逐模型明细（明细日志
// 按 ts 精确过滤）、平均耗时取同一对边界，页面上的数字彼此自洽。
//
// 代价是窗口起点最多早于"此刻减 N 小时"不到一小时；对 24h/7d 档位可忽略。
func channelStatusWindowBounds(now time.Time, seconds int64) (from, until int64) {
	return now.Add(-time.Duration(seconds) * time.Second).Truncate(time.Hour).Unix(), now.Unix()
}

// channelStatusBucketStats 把小时桶按 group_key 汇总成窗口总数。
//
// 只接受 `hour` 粒度的桶：日粒度桶的起点被上卷到当天 00:00，边界桶同样会整份计入
// （与上面同一条坑）。逐日序列另由小时桶在内存里折叠，保证 Σ逐日 == 窗口总数。
func channelStatusBucketStats(buckets []model.PBRStatBucket) map[string]channelStatusSummary {
	totals := make(map[string]channelStatusSummary, len(buckets))
	for _, b := range buckets {
		acc := totals[b.GroupKey]
		acc.Requests += b.Requests
		acc.Successes += b.Successes
		acc.EstimatedCost += b.EstimatedCost
		totals[b.GroupKey] = acc
	}
	for key, acc := range totals {
		acc.Failures = acc.Requests - acc.Successes
		if acc.Requests > 0 {
			acc.SuccessRate = float64(acc.Successes) / float64(acc.Requests)
		}
		totals[key] = acc
	}
	return totals
}

// channelRefsAndRuntime 遍历全部车道，收集该渠道的配置引用与运行态成员。
//
// 运行态取自进程内注册表（route.Default.Get，**只读不创建**）：车道登记过运行态就用
// 真实快照；没登记过（从未有请求选中过它）按"冷启"合成——此时冷却/熔断本来就为空，
// 合成的语义与真实空运行态一致，避免读端点产生副作用。
func channelRefsAndRuntime(channel *model.Channel) (refs []channelStatusLaneRef, members []channelStatusMember, laneCount, totalMembers, availableCount int) {
	lanes, err := model.ListLanes()
	if err != nil {
		return nil, nil, 0, 0, 0
	}
	settings := route.CurrentCircuitSettings()
	seenLanes := map[string]bool{}
	refs = []channelStatusLaneRef{}
	members = []channelStatusMember{}

	for i := range lanes {
		laneName := lanes[i].Name
		resolved, resolveErr := model.ResolveRoute(laneName)
		if resolveErr != nil || resolved == nil || resolved.Source != model.RouteSourceExplicit {
			continue
		}
		referenced := false
		for idx := range resolved.Members {
			if resolved.Members[idx].ChannelId == channel.Id {
				referenced = true
				break
			}
		}
		if !referenced {
			continue
		}
		seenLanes[laneName] = true

		rt := route.Default.Get(laneName)
		var snapshot route.HealthSnapshot
		if rt != nil {
			snapshot = rt.Health(resolved, settings)
		}

		for idx := range resolved.Members {
			m := &resolved.Members[idx]
			if m.ChannelId != channel.Id {
				continue
			}
			refs = append(refs, channelStatusLaneRef{
				Lane:          laneName,
				Member:        m.Channel + "/" + m.UpstreamModel,
				Model:         m.Model,
				UpstreamModel: m.UpstreamModel,
				Priority:      m.Priority,
				Enabled:       !m.Disabled,
			})
			totalMembers++

			entry := channelStatusMember{Lane: laneName}
			found := false
			for j := range snapshot.Members {
				if snapshot.Members[j].Channel == channel.Name && snapshot.Members[j].UpstreamModel == m.UpstreamModel {
					entry.MemberHealth = snapshot.Members[j]
					found = true
					break
				}
			}
			if !found {
				// 该车道尚未登记运行态（从未有请求走完选路）：冷启形状。
				entry.MemberHealth = route.MemberHealth{
					Member:        m.Channel + "/" + m.UpstreamModel,
					Channel:       channel.Name,
					UpstreamModel: m.UpstreamModel,
					Circuit:       route.CircuitClosed,
					Available:     !m.Disabled,
					Enabled:       !m.Disabled,
				}
			}
			if entry.Available {
				availableCount++
			}
			members = append(members, entry)
		}
	}
	sort.SliceStable(refs, func(a, b int) bool {
		if refs[a].Lane != refs[b].Lane {
			return refs[a].Lane < refs[b].Lane
		}
		return refs[a].Priority > refs[b].Priority
	})
	return refs, members, len(seenLanes), totalMembers, availableCount
}

// channelWindowStats 窗口内的统计汇总与逐日序列（小时聚合表，对 prune 免疫）。
//
// 汇总读**小时桶**再相加（口径见 channelStatusWindowBounds）；逐日序列由同一批小时桶
// 在内存里按天折叠，所以 Σ逐日恒等于 summary.Requests，图表与概览不会互相打脸。
// 窗口边界所在的两天是"部分天"，其数值只覆盖窗口内的小时。
func channelWindowStats(channelName string, from, until int64) (channelStatusSummary, []gin.H) {
	summary := channelStatusSummary{}
	daily := []gin.H{}
	buckets, err := model.AggregatePBRChannelStats(channelName, from, until, "hour")
	if err != nil {
		return summary, daily
	}
	type dayAcc struct {
		requests  int64
		successes int64
		cost      float64
	}
	days := map[int64]*dayAcc{}
	for _, b := range buckets {
		summary.Requests += b.Requests
		summary.Successes += b.Successes
		summary.EstimatedCost += b.EstimatedCost
		day := b.BucketTs / 86400 * 86400
		acc := days[day]
		if acc == nil {
			acc = &dayAcc{}
			days[day] = acc
		}
		acc.requests += b.Requests
		acc.successes += b.Successes
		acc.cost += b.EstimatedCost
	}
	// 逐日序列按时间正序输出（旧 → 新）。
	ordered := make([]int64, 0, len(days))
	for day := range days {
		ordered = append(ordered, day)
	}
	sort.Slice(ordered, func(a, b int) bool { return ordered[a] < ordered[b] })
	for _, day := range ordered {
		acc := days[day]
		daily = append(daily, gin.H{
			"date":           time.Unix(day, 0).Format("2006-01-02"),
			"requests":       acc.requests,
			"successes":      acc.successes,
			"failures":       acc.requests - acc.successes,
			"estimated_cost": acc.cost,
		})
	}
	summary.Failures = summary.Requests - summary.Successes
	if summary.Requests > 0 {
		summary.SuccessRate = float64(summary.Successes) / float64(summary.Requests)
	}
	return summary, daily
}

// channelModelStats 该渠道在窗口内**按模型**的表现（渠道状态页的主表；api-spec §6.12）。
//
// 一行 = 该渠道在这个上游模型上服务过的全部请求：成功率、平均延迟、平均首字延迟、
// 输出速度（tokens/s）与输出 token 数。数据源是**明细日志**（会被 /logs/prune 影响，
// 与 summary.avg_* 同一取舍）；按 `upstream_model` 归组——那是这个渠道真正发出去的模型名。
//
// 输出速度口径：`Σcompletion_tokens / Σ生成耗时`，生成耗时 = 流式取 `max(total_ms - ttft_ms, 1)`、
// 非流式取 `total_ms`（毫秒）。这是"输出速度"最诚实的定义：把首字等待排除在外。
func channelModelStats(channelName string, from, until int64) []gin.H {
	if model.DB == nil || strings.TrimSpace(channelName) == "" {
		return []gin.H{}
	}
	var rows []struct {
		Model      string
		Requests   int64
		Successes  int64
		AvgTotalMs float64
		AvgTTFTMs  float64
		TokensOut  int64
		GenMs      float64
	}
	query := model.DB.Table("pbr_request_logs").
		Select("upstream_model AS model, COUNT(*) AS requests, "+
			"SUM(CASE WHEN success THEN 1 ELSE 0 END) AS successes, "+
			"COALESCE(AVG(total_ms), 0) AS avg_total_ms, "+
			"COALESCE(AVG(CASE WHEN is_stream THEN ttft_ms END), 0) AS avg_ttft_ms, "+
			"COALESCE(SUM(completion_tokens), 0) AS tokens_out, "+
			"COALESCE(SUM(CASE WHEN is_stream THEN MAX(total_ms - ttft_ms, 1) ELSE total_ms END), 0) AS gen_ms").
		Where("member_channel_name = ?", strings.TrimSpace(channelName)).
		Group("upstream_model").
		Order("requests desc")
	if from > 0 {
		query = query.Where("ts >= ?", from)
	}
	if until > 0 {
		query = query.Where("ts < ?", until)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return []gin.H{}
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		rate := 0.0
		if r.Requests > 0 {
			rate = float64(r.Successes) / float64(r.Requests)
		}
		tps := 0.0
		if r.GenMs > 0 {
			tps = float64(r.TokensOut) * 1000 / r.GenMs
		}
		out = append(out, gin.H{
			"model":        r.Model,
			"requests":     r.Requests,
			"successes":    r.Successes,
			"failures":     r.Requests - r.Successes,
			"success_rate": rate,
			"avg_total_ms": r.AvgTotalMs,
			"avg_ttft_ms":  r.AvgTTFTMs,
			"output_tps":   tps,
			"tokens_out":   r.TokensOut,
		})
	}
	return out
}

// recentChannelFailures 该渠道最近 N 条失败明细（只读元数据，不含请求/响应正文）。
func recentChannelFailures(channelName string) []gin.H {
	success := false
	entries, err := model.ListPBRRequestLogs(model.PBRRequestLogFilter{
		Channel: channelName,
		Success: &success,
		Limit:   channelStatusRecentLimit,
	})
	if err != nil {
		return []gin.H{}
	}
	if len(entries) > channelStatusRecentLimit {
		entries = entries[:channelStatusRecentLimit]
	}
	out := make([]gin.H, 0, len(entries))
	for i := range entries {
		e := &entries[i]
		out = append(out, gin.H{
			"id":             e.Id,
			"ts":             time.Unix(e.Ts, 0).UTC().Format(time.RFC3339),
			"lane":           e.LaneName,
			"model":          e.RequestModel,
			"upstream_model": e.UpstreamModel,
			"http_status":    e.HTTPStatus,
			"error_kind":     e.ErrorKind,
			"error_summary":  e.ErrorSummary,
		})
	}
	return out
}

// channelProbeHistory 该渠道最近 N 条探活记录（新 → 旧）。
func channelProbeHistory(channelId int) []gin.H {
	rows, err := model.RecentProbeLogs(channelId, channelStatusRecentLimit)
	if err != nil {
		return []gin.H{}
	}
	out := make([]gin.H, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		out = append(out, gin.H{
			"ts":             time.Unix(r.Ts, 0).UTC().Format(time.RFC3339),
			"upstream_model": r.UpstreamModel,
			"status":         r.Status,
			"duration_ms":    r.DurationMs,
			"http_status":    r.HTTPStatus,
			"error_kind":     r.ErrorKind,
			"msg":            r.Msg,
		})
	}
	return out
}

// channelUpstreamUpdateSnapshot 上游模型变动提示（渠道设置里上次探测的快照，不访问上游）。
// 上次探测结果落在渠道的 other_settings（上游检测分组）里，本端点只读它、不触发探测。
func channelUpstreamUpdateSnapshot(channel *model.Channel) gin.H {
	var lastCheck any
	pendingAdd := []string{}
	pendingRemove := []string{}
	settings := channel.GetOtherSettings()
	if settings.UpstreamModelUpdateLastCheckTime > 0 {
		lastCheck = time.Unix(settings.UpstreamModelUpdateLastCheckTime, 0).UTC().Format(time.RFC3339)
	}
	if len(settings.UpstreamModelUpdateLastDetectedModels) > 0 {
		pendingAdd = settings.UpstreamModelUpdateLastDetectedModels
	}
	if len(settings.UpstreamModelUpdateLastRemovedModels) > 0 {
		pendingRemove = settings.UpstreamModelUpdateLastRemovedModels
	}
	return gin.H{
		"last_check_ts":  lastCheck,
		"pending_add":    pendingAdd,
		"pending_remove": pendingRemove,
	}
}

// GetChannelStatusByName GET /api/channels/{name}/status：单渠道状态详情（api-spec §6.12）。
func GetChannelStatusByName(c *gin.Context) {
	channel, err := findChannelByName(c.Param("name"))
	if err != nil {
		apierr.NotFound(c, apierr.CodeChannelNotFound, "channel '"+c.Param("name")+"' not found", "GET /api/v1/channels")
		return
	}
	window, seconds, ok := parseChannelStatusWindow(c)
	if !ok {
		return
	}
	from, until := channelStatusWindowBounds(time.Now(), seconds)

	refs, members, _, totalMembers, availableCount := channelRefsAndRuntime(channel)
	summary, daily := channelWindowStats(channel.Name, from, until)
	avgTotal, avgTTFT, err := model.ChannelLatencyAverages(channel.Name, from, until)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	summary.AvgTotalMs = avgTotal
	summary.AvgTTFTMs = avgTTFT

	c.JSON(http.StatusOK, gin.H{
		"channel":   channelResponse(channel),
		"reference": gin.H{"lanes": refs},
		"runtime": gin.H{
			"member_count":    totalMembers,
			"available_count": availableCount,
			"members":         members,
		},
		"window":           window,
		"summary":          summary,
		"models":           channelModelStats(channel.Name, from, until),
		"daily":            daily,
		"recent_failures":  recentChannelFailures(channel.Name),
		"probe_history":    channelProbeHistory(channel.Id),
		"upstream_updates": channelUpstreamUpdateSnapshot(channel),
	})
}

// ListChannelStatus GET /api/channels/status：全部渠道的状态汇总（api-spec §6.12）。
func ListChannelStatus(c *gin.Context) {
	window, seconds, ok := parseChannelStatusWindow(c)
	if !ok {
		return
	}
	from, until := channelStatusWindowBounds(time.Now(), seconds)

	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	// 一次取全量渠道的窗口小时桶，再按渠道名分发（渠道数量级很小）。
	// 必须用 `hour` 粒度：日粒度会把边界那天整份算进来（见 channelStatusWindowBounds）。
	bucketTotals := map[string]channelStatusSummary{}
	if buckets, err := model.AggregatePBRStatsFromHourly(from, until, "hour", "channel"); err == nil {
		bucketTotals = channelStatusBucketStats(buckets)
	}
	ids := make([]int, 0, len(channels))
	for _, ch := range channels {
		ids = append(ids, ch.Id)
	}
	latestProbes, _ := model.LatestProbeLogByChannel(ids)

	items := make([]gin.H, 0, len(channels))
	for _, ch := range channels {
		_, members, laneCount, memberCount, availableCount := channelRefsAndRuntime(ch)
		lastErrorKind := ""
		degraded := false
		for _, m := range members {
			if !m.Available {
				degraded = true
			}
			if string(m.LastErrorKind) != "" {
				lastErrorKind = string(m.LastErrorKind)
			}
		}
		// channelStatusBucketStats 已把 failures/success_rate 一并算好，
		// 这里直接取用，避免同一个口径在两处各写一份。
		acc := bucketTotals[ch.Name]
		item := gin.H{
			"channel":         ch.Name,
			"enabled":         ch.Status == common.ChannelStatusEnabled,
			"type":            ChannelTypeSlug(ch.Type),
			"lane_count":      laneCount,
			"member_count":    memberCount,
			"available_count": availableCount,
			"degraded":        degraded,
			"requests":        acc.Requests,
			"failures":        acc.Failures,
			"success_rate":    acc.SuccessRate,
			"estimated_cost":  acc.EstimatedCost,
			"last_error_kind": lastErrorKind,
		}
		if probe, ok := latestProbes[ch.Id]; ok {
			item["last_probe"] = gin.H{
				"ts":          time.Unix(probe.Ts, 0).UTC().Format(time.RFC3339),
				"status":      probe.Status,
				"duration_ms": probe.DurationMs,
			}
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"window": window, "items": items})
}
