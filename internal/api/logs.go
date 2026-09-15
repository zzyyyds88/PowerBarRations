package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"pbr/internal/apierr"
	"pbr/model"

	"github.com/gin-gonic/gin"
)

// 请求日志与统计（api-spec §5.5，design-v1 §8）。
//
// 只读元数据：没有请求/响应正文，没有余额与扣费。成本是"折算"，不参与准入。

// ListLogs GET /api/v1/logs
//
// 过滤：lane、channel、key、model、success、since、until、cursor、limit。
func ListLogs(c *gin.Context) {
	limit, cursor, err := pageParams(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	filter := model.PBRRequestLogFilter{Limit: limit}
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
		apierr.Validation(c, "since must be RFC3339 or unix seconds")
		return
	}
	until, err := parseTimeQuery(c.Query("until"))
	if err != nil {
		apierr.Validation(c, "until must be RFC3339 or unix seconds")
		return
	}
	filter.Since, filter.Until = since, until
	if cursor != "" {
		filter.BeforeId, _ = strconv.Atoi(cursor)
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
// granularity=hour|day，from/to 为 Unix 秒，group_by=lane|channel|key|model。
func GetStats(c *gin.Context) {
	granularity := strings.ToLower(strings.TrimSpace(c.DefaultQuery("granularity", "hour")))
	if granularity != "hour" && granularity != "day" {
		apierr.Validation(c, "granularity must be hour or day")
		return
	}
	groupBy := strings.ToLower(strings.TrimSpace(c.DefaultQuery("group_by", "lane")))
	switch groupBy {
	case "lane", "channel", "key", "model":
	default:
		apierr.Validation(c, "group_by must be lane, channel, key or model")
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
	buckets, err := model.AggregatePBRStats(from, to, granularity, groupBy)
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
		"upstream_model":     entry.UpstreamModel,
		"key_name":           entry.TokenName,
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
	if dryRun(c) {
		// 干跑也留审计痕迹（api-spec §2.6 的 dry_run 字段）
		writeAudit(c, "prune", "logs", "logs")
		c.JSON(http.StatusOK, gin.H{
			"dry_run":        true,
			"valid":          true,
			"before":         rfc3339(before),
			"retention_days": retentionDays,
			"deleted":        0,
			"would_delete":   count,
			"diff":           gin.H{"logs": gin.H{"remove": count}},
		})
		return
	}

	deleted, err := model.PrunePBRRequestLogsBefore(before)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	writeAudit(c, "prune", "logs", "logs")
	c.JSON(http.StatusOK, gin.H{
		"deleted":        deleted,
		"before":         rfc3339(before),
		"retention_days": retentionDays,
	})
}
