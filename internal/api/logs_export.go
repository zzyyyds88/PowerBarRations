package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 请求日志导出（api-spec §5.5.1、design-v1 §8）。
//
// 只读：按 /api/logs 的过滤条件把明细行流式写成 CSV / NDJSON，不落临时文件、
// 不改动任何日志数据。命中行数与时间跨度必须在写出响应体**之前**校验——
// 流式响应一旦发出 200 头部，就再也无法用 400 + error.code 告诉调用方
// "超限了"，只能给出一个截断文件。

const (
	// exportRowLimit 是 api-spec §5.5.1 规定的命中行数上限。
	exportRowLimit = 100000
	// exportMaxRangeSeconds 是导出区间上限（31 天，含端点）。
	exportMaxRangeSeconds = 31 * 24 * 60 * 60
	// exportPageSize 是分页读取明细的批大小（复用 ListPBRRequestLogs 的游标模式）。
	exportPageSize = 1000
	// exportFlushEvery 是流式写出的刷新间隔（行）：长导出不必等全部结束才见数据。
	exportFlushEvery = 500
	// exportAuditNameRunes 是审计 name 列的可读长度上限（PBRAuditLog.Name varchar(128)）。
	exportAuditNameRunes = 128

	exportFormatCSV    = "csv"
	exportFormatNDJSON = "ndjson"

	// 导出专属稳定错误码（api-spec §5.5.1；不属于 §3 的通用错误码表）。
	exportCodeRangeTooLarge = "export_range_too_large"
	exportCodeTooLarge      = "export_too_large"
)

// exportCSVColumns 是 CSV 固定列顺序（api-spec §5.5.1，不得增删或重排）。
// 不含 attempts（JSON）与 error_summary；NDJSON 才输出完整行。
var exportCSVColumns = []string{
	"ts",
	"request_id",
	"lane",
	"request_model",
	"route_source",
	"channel",
	"upstream_model",
	"key_name",
	"type",
	"inbound_format",
	"success",
	"http_status",
	"error_kind",
	"prompt_tokens",
	"completion_tokens",
	"cache_read_tokens",
	"cache_write_tokens",
	"reasoning_tokens",
	"ttft_ms",
	"total_ms",
	"is_stream",
	"total_attempts",
	"estimated_cost",
	"price_band",
	"session_id",
	"session_key",
	"client",
	"subagent",
	"parent_subagent",
	"parent_session_id",
	"context_tokens",
	"context_window",
	"context_counted",
	"context_status",
	"masked_count",
	"mask_blocked",
	"mask_status",
	"mask_took_ms",
}

// errExportLimitReached 只用于中断"已到行数上限"的翻页遍历（并发新增行时的防御），
// 不是对外错误：命中上限的 400 在写出前已经用 count 判过，这里只是不越界。
var errExportLimitReached = errors.New("export row limit reached")

// ExportLogs GET /api/v1/logs/export（由 G5 统一注册路由）。
//
// 参数：format=csv|ndjson（默认 csv）、from/to（RFC3339，必填）、
// lane/channel/key/model/success/error_kind（与 /api/logs 同口径）。
func ExportLogs(c *gin.Context) {
	exportLogs(c, exportRowLimit)
}

// exportLogs 是 ExportLogs 的可注入阈值变体：rowLimit 只影响行数上限，
// 生产取值恒为 exportRowLimit；单测用小阈值覆盖 export_too_large 分支，
// 避免为了一个边界插入十万行。不引入全局可变状态。
func exportLogs(c *gin.Context, rowLimit int) {
	format, err := parseExportFormat(c.Query("format"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	from, to, err := parseExportWindow(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	// 复用 /api/logs 的过滤解析（lane/channel/key/model/success），保证两个端点
	// 的过滤口径不分叉；导出契约用 from/to 定位时间窗，覆盖 since/until。
	filter, err := parseLogFilter(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	filter.Since, filter.Until = from, to
	errorKind := strings.TrimSpace(c.Query("error_kind"))

	// 先确定命中行数，再决定是否允许写出（超限必须能在响应体发出前 400）。
	// error_kind 目前不是 model.PBRRequestLogFilter 的维度（G2 不拥有 model/），
	// 因此有该过滤时复用 ListPBRRequestLogs 分页读取后在内存收窄，不复制 SQL。
	var pending []model.PBRRequestLog
	var matched int64
	if errorKind == "" {
		matched, err = model.CountPBRRequestLogs(filter)
	} else {
		pending, matched, err = scanExportLogs(filter, errorKind, rowLimit)
	}
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if matched > int64(rowLimit) {
		writeAPIError(c, &apiError{
			status:  http.StatusBadRequest,
			code:    exportCodeTooLarge,
			message: fmt.Sprintf("export matched %d rows, exceeding the %d row limit", matched, rowLimit),
			hint:    "narrow from/to or add filters (lane/channel/key/model/success/error_kind), then retry",
			details: gin.H{"matched": matched, "limit": rowLimit},
		})
		return
	}

	setExportHeaders(c, format, from, to)
	rows, writeErr := streamExportLogs(c, format, filter, errorKind, pending, rowLimit)
	if writeErr != nil && !errors.Is(writeErr, errExportLimitReached) {
		// 响应体已经开始流式写出，不能再写 JSON 错误包络；把错误交给 gin 的
		// 上下文错误链（访问日志/中间件可见），调用方看到的是明确截断的流。
		_ = c.Error(writeErr)
	}

	// 导出是读操作，仍留审计（api-spec §5.5.1）：行数 + 过滤摘要，不落导出内容。
	summary := exportAuditName(format, from, to, filter, errorKind, rows)
	writeAudit(c, "export", "logs", summary, gin.H{
		"format": format,
		"rows":   rows,
		"from":   rfc3339(from),
		"to":     rfc3339(to),
		"filter": exportFilterDigest(filter, errorKind),
	})
}

// parseExportFormat 解析 format，默认 csv（api-spec §5.5.1）。
func parseExportFormat(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", exportFormatCSV:
		return exportFormatCSV, nil
	case exportFormatNDJSON:
		return exportFormatNDJSON, nil
	default:
		return "", &apiError{
			code:    apierr.CodeValidationFailed,
			message: "format must be csv or ndjson",
		}
	}
}

// parseExportWindow 解析必填的 from/to（RFC3339）并校验跨度上限。
// 导出契约只认 RFC3339（api-spec §5.5.1），不像 /api/logs 的 since/until 那样
// 兼容 Unix 秒——导出是给人/脚本下载的，时间口径不能有两个。
func parseExportWindow(c *gin.Context) (int64, int64, error) {
	rawFrom := strings.TrimSpace(c.Query("from"))
	rawTo := strings.TrimSpace(c.Query("to"))
	if rawFrom == "" || rawTo == "" {
		return 0, 0, &apiError{
			code:    apierr.CodeValidationFailed,
			message: "from and to are required (RFC3339)",
		}
	}
	from, err := time.Parse(time.RFC3339, rawFrom)
	if err != nil {
		return 0, 0, &apiError{
			code:    apierr.CodeValidationFailed,
			message: "from must be RFC3339",
		}
	}
	to, err := time.Parse(time.RFC3339, rawTo)
	if err != nil {
		return 0, 0, &apiError{
			code:    apierr.CodeValidationFailed,
			message: "to must be RFC3339",
		}
	}
	if !to.After(from) {
		return 0, 0, &apiError{
			code:    apierr.CodeValidationFailed,
			message: "to must be after from",
		}
	}
	if to.Sub(from) > time.Duration(exportMaxRangeSeconds)*time.Second {
		return 0, 0, &apiError{
			status:  http.StatusBadRequest,
			code:    exportCodeRangeTooLarge,
			message: fmt.Sprintf("export range exceeds %d days", exportMaxRangeSeconds/86400),
			hint:    "narrow from/to to at most 31 days, then retry",
			details: gin.H{
				"max_days": 31,
				"from":     rfc3339(from.Unix()),
				"to":       rfc3339(to.Unix()),
			},
		}
	}
	return from.Unix(), to.Unix(), nil
}

// setExportHeaders 按 api-spec §5.5.1 设置响应头。
// 文件名只由时间戳拼出，不含调用方原文，避免响应头注入。
func setExportHeaders(c *gin.Context, format string, from, to int64) {
	contentType := "text/csv; charset=utf-8"
	ext := "csv"
	if format == exportFormatNDJSON {
		contentType = "application/x-ndjson"
		ext = "ndjson"
	}
	filename := fmt.Sprintf("pbr-logs-%s-%s.%s", exportFilenameTime(from), exportFilenameTime(to), ext)
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
}

// exportFilenameTime 生成文件名里的 UTC 紧凑时间（20260101T000000Z）。
func exportFilenameTime(sec int64) string {
	return time.Unix(sec, 0).UTC().Format("20060102T150405Z")
}

// streamExportLogs 把命中行写成目标格式并返回实际写出行数。
// 不缓冲整份导出：CSV 用 encoding/csv 逐行写 c.Writer，NDJSON 逐行写
// JSON + 换行，并周期性 Flush。
func streamExportLogs(c *gin.Context, format string, filter model.PBRRequestLogFilter, errorKind string, buffered []model.PBRRequestLog, rowLimit int) (int, error) {
	sink := newLogExportSink(c, format)
	if err := sink.writeHeader(); err != nil {
		return 0, err
	}
	emit := func(entry *model.PBRRequestLog) error {
		// 计数与写出之间可能有并发新增（count 之后才落库的行）：不越界写出。
		if sink.rows >= rowLimit {
			return errExportLimitReached
		}
		if err := sink.write(entry); err != nil {
			return err
		}
		if sink.rows%exportFlushEvery == 0 {
			return sink.flush()
		}
		return nil
	}
	if errorKind != "" {
		for i := range buffered {
			if err := emit(&buffered[i]); err != nil {
				return sink.rows, err
			}
		}
	} else if err := forEachExportLog(filter, emit); err != nil {
		return sink.rows, err
	}
	if err := sink.flush(); err != nil {
		return sink.rows, err
	}
	return sink.rows, nil
}

// logExportSink 是流式写出器：CSV 与 NDJSON 共用写入口。
type logExportSink struct {
	c    *gin.Context
	csv  *csv.Writer
	rows int
}

func newLogExportSink(c *gin.Context, format string) *logExportSink {
	sink := &logExportSink{c: c}
	if format == exportFormatCSV {
		sink.csv = csv.NewWriter(c.Writer)
	}
	return sink
}

func (s *logExportSink) writeHeader() error {
	if s.csv == nil {
		return nil
	}
	return s.csv.Write(exportCSVColumns)
}

func (s *logExportSink) write(entry *model.PBRRequestLog) error {
	if s.csv != nil {
		if err := s.csv.Write(exportCSVRow(entry)); err != nil {
			return err
		}
		s.rows++
		return nil
	}
	encoded, err := json.Marshal(logResponse(entry))
	if err != nil {
		return err
	}
	if _, err := s.c.Writer.Write(append(encoded, '\n')); err != nil {
		return err
	}
	s.rows++
	return nil
}

func (s *logExportSink) flush() error {
	if s.csv != nil {
		s.csv.Flush()
		if err := s.csv.Error(); err != nil {
			return err
		}
	}
	s.c.Writer.Flush()
	return nil
}

// scanExportLogs 分页扫描基础过滤命中的明细，在内存里按 error_kind 收窄。
// 返回按 id 倒序的行（仅保留前 keep 行，足够写出一份不超限的导出）与**精确**
// 命中数（用于 export_too_large 的提示）。
func scanExportLogs(filter model.PBRRequestLogFilter, errorKind string, keep int) ([]model.PBRRequestLog, int64, error) {
	capacity := keep
	if capacity > exportPageSize {
		capacity = exportPageSize
	}
	if capacity < 0 {
		capacity = 0
	}
	rows := make([]model.PBRRequestLog, 0, capacity)
	var matched int64
	err := forEachExportLog(filter, func(entry *model.PBRRequestLog) error {
		if entry.ErrorKind != errorKind {
			return nil
		}
		matched++
		if len(rows) < keep {
			rows = append(rows, *entry)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return rows, matched, nil
}

// forEachExportLog 用 model 层公开的 ListPBRRequestLogs 游标模式分页遍历，
// 不复制过滤 SQL；每页按 id 倒序，翻页条件沿用 BeforeId。
func forEachExportLog(filter model.PBRRequestLogFilter, fn func(*model.PBRRequestLog) error) error {
	filter.Page = 0
	filter.PageSize = 0
	for {
		pageFilter := filter
		pageFilter.Limit = exportPageSize
		entries, err := model.ListPBRRequestLogs(pageFilter)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return nil
		}
		processed := 0
		for i := range entries {
			// 游标模式会多取 1 行探测下一页，这里只消费本页。
			if processed >= exportPageSize {
				break
			}
			if err := fn(&entries[i]); err != nil {
				return err
			}
			filter.BeforeId = entries[i].Id
			processed++
		}
		if len(entries) <= exportPageSize {
			return nil
		}
	}
}

// exportCSVRow 按 exportCSVColumns 的固定顺序生成一行。
// 布尔值用 true/false（CSV 消费方与 JSON 口径一致），时间用 RFC3339。
func exportCSVRow(entry *model.PBRRequestLog) []string {
	return []string{
		rfc3339(entry.Ts),
		// request_id：PBR 的明细主键（api-spec §4.4 的 id）；日志表没有独立的
		// 请求追踪号，导出列名沿用契约。
		strconv.Itoa(entry.Id),
		entry.LaneName,
		entry.RequestModel,
		entry.RouteSource,
		entry.MemberChannelName,
		entry.UpstreamModel,
		entry.TokenName,
		strconv.Itoa(entry.Type),
		entry.InboundFormat,
		strconv.FormatBool(entry.Success),
		strconv.Itoa(entry.HTTPStatus),
		entry.ErrorKind,
		strconv.Itoa(entry.PromptTokens),
		strconv.Itoa(entry.CompletionTokens),
		strconv.Itoa(entry.CacheReadTokens),
		strconv.Itoa(entry.CacheWriteTokens),
		strconv.Itoa(entry.ReasoningTokens),
		strconv.FormatInt(entry.TTFTMs, 10),
		strconv.FormatInt(entry.TotalMs, 10),
		strconv.FormatBool(entry.IsStream),
		strconv.Itoa(entry.TotalAttempts),
		strconv.FormatFloat(entry.EstimatedCost, 'f', -1, 64),
		strconv.Itoa(entry.PriceBand),
		entry.SessionID,
		entry.SessionKey,
		entry.Client,
		entry.Subagent,
		entry.ParentSubagent,
		entry.ParentSessionID,
		strconv.Itoa(entry.ContextTokens),
		strconv.Itoa(entry.ContextWindow),
		strconv.FormatBool(entry.ContextCounted),
		entry.ContextStatus,
		strconv.Itoa(entry.MaskedCount),
		strconv.FormatBool(entry.MaskBlocked),
		entry.MaskStatus,
		strconv.FormatInt(entry.MaskTookMs, 10),
	}
}

// exportAuditName 生成审计 name 列的可读摘要（行数与过滤摘要）。
// PBRAuditLog.Name 只有 128 字符，超长截断；完整结构化摘要另以 digest 落库。
func exportAuditName(format string, from, to int64, filter model.PBRRequestLogFilter, errorKind string, rows int) string {
	parts := []string{
		format,
		"rows=" + strconv.Itoa(rows),
		"from=" + rfc3339(from),
		"to=" + rfc3339(to),
	}
	if filter.Lane != "" {
		parts = append(parts, "lane="+filter.Lane)
	}
	if filter.Channel != "" {
		parts = append(parts, "channel="+filter.Channel)
	}
	if filter.Key != "" {
		parts = append(parts, "key="+filter.Key)
	}
	if filter.Model != "" {
		parts = append(parts, "model="+filter.Model)
	}
	if filter.Client != "" {
		parts = append(parts, "client="+filter.Client)
	}
	if filter.Session != "" {
		parts = append(parts, "session="+filter.Session)
	}
	if filter.Success != nil {
		parts = append(parts, "success="+strconv.FormatBool(*filter.Success))
	}
	if errorKind != "" {
		parts = append(parts, "error_kind="+errorKind)
	}
	name := strings.Join(parts, " ")
	if runes := []rune(name); len(runes) > exportAuditNameRunes {
		name = string(runes[:exportAuditNameRunes])
	}
	return name
}

// exportFilterDigest 是审计 after_digest 的结构化摘要（不含导出内容）。
func exportFilterDigest(filter model.PBRRequestLogFilter, errorKind string) gin.H {
	digest := gin.H{
		"lane":    filter.Lane,
		"channel": filter.Channel,
		"key":     filter.Key,
		"model":   filter.Model,
		"client":  filter.Client,
		"session": filter.Session,
	}
	if filter.Success != nil {
		digest["success"] = *filter.Success
	}
	if errorKind != "" {
		digest["error_kind"] = errorKind
	}
	return digest
}
