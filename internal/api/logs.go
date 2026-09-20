package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 请求日志与统计（api-spec §5.5，design-v1 §8）。
//
// 只读元数据：没有请求/响应正文，没有余额与扣费。成本是"折算"，不参与准入。

// parseLogFilter 解析日志过滤条件（偏移与游标分支共用，避免口径分叉）。
// 不解析分页参数（page/page_size/cursor/limit）——由各分支自行处理。
func parseLogFilter(c *gin.Context) (model.PBRRequestLogFilter, error) {
	filter := model.PBRRequestLogFilter{}
	filter.Lane = strings.TrimSpace(c.Query("lane"))
	filter.Channel = strings.TrimSpace(c.Query("channel"))
	filter.Key = strings.TrimSpace(c.Query("key"))
	filter.Model = strings.TrimSpace(c.Query("model"))
	if raw := strings.TrimSpace(c.Query("success")); raw != "" {
		value := raw == "1" || strings.EqualFold(raw, "true")
		filter.Success = &value
	}
	since, err := parseTimeQuery(c.Query("since"))
	if err != nil {
		return filter, &apiError{code: apierr.CodeValidationFailed, message: "since must be RFC3339 or unix seconds"}
	}
	until, err := parseTimeQuery(c.Query("until"))
	if err != nil {
		return filter, &apiError{code: apierr.CodeValidationFailed, message: "until must be RFC3339 or unix seconds"}
	}
	filter.Since, filter.Until = since, until
	return filter, nil
}

// ListLogs GET /api/v1/logs
//
// 过滤：lane、channel、key、model、success、since、until。
// 分页双模式（api-spec §5.5）：传 page+page_size 走偏移（响应 items/total/page/page_size）；
// 传 cursor+limit 走游标（响应 items/next_cursor，向后兼容）。由是否传 page 判定。
func ListLogs(c *gin.Context) {
	if rawPage := strings.TrimSpace(c.Query("page")); rawPage != "" {
		listLogsPaged(c, rawPage)
		return
	}
	listLogsCursor(c)
}

// listLogsCursor 游标分页（向后兼容）。
func listLogsCursor(c *gin.Context) {
	limit, cursor, err := pageParams(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	filter, err := parseLogFilter(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	filter.Limit = limit
	if cursor != "" {
		id, convErr := strconv.Atoi(cursor)
		if convErr != nil {
			apierr.Validation(c, "cursor is not a valid cursor")
			return
		}
		filter.BeforeId = id
	}

	entries, err := model.ListPBRRequestLogs(filter)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	// 游标必须与读取端一致：读取端走 pageParams→decodeCursor（base64），
	// 这里写裸十进制 id 会导致翻页永远回到第一页。
	var nextCursor any
	if len(entries) > limit {
		nextCursor = encodeCursor(strconv.Itoa(entries[limit-1].Id))
		entries = entries[:limit]
	}
	items := make([]gin.H, 0, len(entries))
	for i := range entries {
		items = append(items, logResponse(&entries[i]))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor})
}

// listLogsPaged 偏移分页（供控制台跳页/总数）。page_size 上限 100。
func listLogsPaged(c *gin.Context, rawPage string) {
	page, convErr := strconv.Atoi(rawPage)
	if convErr != nil || page <= 0 {
		apierr.Validation(c, "page must be a positive integer")
		return
	}
	pageSize := 50
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed <= 0 {
			apierr.Validation(c, "page_size must be a positive integer")
			return
		}
		pageSize = parsed
	}
	if pageSize > 100 {
		pageSize = 100
	}
	filter, err := parseLogFilter(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	filter.Page = page
	filter.PageSize = pageSize

	total, err := model.CountPBRRequestLogs(filter)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	entries, err := model.ListPBRRequestLogs(filter)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	items := make([]gin.H, 0, len(entries))
	for i := range entries {
		items = append(items, logResponse(&entries[i]))
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetLog GET /api/v1/logs/{id}：含 attempts 链。
func GetLog(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		apierr.Validation(c, "log id must be an integer")
		return
	}
	entry, err := model.GetPBRRequestLogById(id)
	if err != nil {
		apierr.NotFound(c, apierr.CodeLogNotFound, "log '"+c.Param("id")+"' not found", "GET /api/v1/logs")
		return
	}
	c.JSON(http.StatusOK, logResponse(entry))
}

// GetStats GET /api/v1/stats
//
// granularity=hour|day，from/to 为 Unix 秒，group_by=lane|channel|key|model|channel_model。
func GetStats(c *gin.Context) {
	granularity := strings.ToLower(strings.TrimSpace(c.DefaultQuery("granularity", "hour")))
	if granularity != "hour" && granularity != "day" {
		apierr.Validation(c, "granularity must be hour or day")
		return
	}
	groupBy := strings.ToLower(strings.TrimSpace(c.DefaultQuery("group_by", "lane")))
	switch groupBy {
	case "lane", "channel", "key", "model", "channel_model":
	default:
		apierr.Validation(c, "group_by must be lane, channel, key, model or channel_model")
		return
	}
	from, err := parseTimeQuery(c.Query("from"))
	if err != nil {
		apierr.Validation(c, "from must be RFC3339 or unix seconds")
		return
	}
	to, err := parseTimeQuery(c.Query("to"))
	if err != nil {
		apierr.Validation(c, "to must be RFC3339 or unix seconds")
		return
	}
	// 读**小时聚合表**：明细可被 prune 删除，聚合长期保留（design-v1 §16.5）。
	// 读明细会让"清理过一次日志"的实例历史统计凭空消失。
	buckets, err := model.AggregatePBRStatsFromHourly(from, to, granularity, groupBy)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"granularity": granularity,
		"group_by":    groupBy,
		"items":       buckets,
	})
}

func logResponse(entry *model.PBRRequestLog) gin.H {
	var attempts any
	if strings.TrimSpace(entry.Attempts) != "" {
		_ = jsonUnmarshal(entry.Attempts, &attempts)
	}
	if attempts == nil {
		attempts = []any{}
	}
	return gin.H{
		"id":                 entry.Id,
		"ts":                 rfc3339(entry.Ts),
		"lane":               entry.LaneName,
		"request_model":      entry.RequestModel,
		"route_source":       entry.RouteSource,
		"channel":            entry.MemberChannelName,
		"channel_id":         entry.MemberChannelId,
		"upstream_model":     entry.UpstreamModel,
		"key_name":           entry.TokenName,
		"token_id":           entry.TokenId,
		"user_id":            entry.UserId,
		"username":           entry.Username,
		"type":               entry.Type,
		"ip":                 entry.Ip,
		"inbound_format":     entry.InboundFormat,
		"success":            entry.Success,
		"http_status":        entry.HTTPStatus,
		"error_kind":         entry.ErrorKind,
		"error_summary":      entry.ErrorSummary,
		"prompt_tokens":      entry.PromptTokens,
		"completion_tokens":  entry.CompletionTokens,
		"cache_read_tokens":  entry.CacheReadTokens,
		"cache_write_tokens": entry.CacheWriteTokens,
		"reasoning_tokens":   entry.ReasoningTokens,
		"ttft_ms":            entry.TTFTMs,
		"total_ms":           entry.TotalMs,
		"is_stream":          entry.IsStream,
		"attempts":           attempts,
		"total_attempts":     entry.TotalAttempts,
		"estimated_cost":     entry.EstimatedCost,
	}
}

// parseTimeQuery 接受 Unix 秒或 RFC3339（api-spec §2.5 规定对外用 RFC3339）。
// 解析失败必须报错：静默忽略过滤条件会把超出时间窗的数据返回给调用方。
func parseTimeQuery(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return value, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return 0, err
	}
	return parsed.Unix(), nil
}

// PruneLogs POST /api/v1/logs/prune?before=<ts>&dry_run=
//
// 按需清理明细日志（design-v1 §16.5：不引入 cron，由外部触发）。只删明细表，
// 聚合表长期保留。before 省略时按 system/options.log_retention_days 推算
// （默认 30 天）。dry_run 只报将删除的条数，不落库。
func PruneLogs(c *gin.Context) {
	before, err := parseTimeQuery(c.Query("before"))
	if err != nil {
		apierr.Validation(c, "before must be RFC3339 or unix seconds")
		return
	}
	retentionDays := CurrentLogRetentionDays()
	if before <= 0 {
		before = time.Now().AddDate(0, 0, -retentionDays).Unix()
	}

	count, err := model.CountPBRRequestLogsBefore(before)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	// webhook_deliveries 随明细日志同一保留期清理（design-v1 §16.10）。
	webhookCount, err := model.CountWebhookDeliveriesBefore(before)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if dryRun(c) {
		// 干跑也留审计痕迹（api-spec §2.6 的 dry_run 字段）
		writeAudit(c, "prune", "logs", "logs")
		c.JSON(http.StatusOK, gin.H{
			"dry_run":                         true,
			"valid":                           true,
			"before":                          rfc3339(before),
			"retention_days":                  retentionDays,
			"deleted":                         0,
			"would_delete":                    count,
			"would_delete_webhook_deliveries": webhookCount,
			"diff":                            gin.H{"logs": gin.H{"remove": count}},
		})
		return
	}

	deleted, err := model.PrunePBRRequestLogsBefore(before)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	webhookDeleted, err := model.PruneWebhookDeliveriesBefore(before)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	writeAudit(c, "prune", "logs", "logs")
	c.JSON(http.StatusOK, gin.H{
		"deleted":        deleted,
		"before":         rfc3339(before),
		"retention_days": retentionDays,
		// 投递日志清理条数（additive；design-v1 §16.10）
		"webhook_deliveries_deleted": webhookDeleted,
	})
}
