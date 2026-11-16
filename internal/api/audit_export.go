package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 管理面导出（api-spec §5.5.2）：审计 / 渠道 / 客户端密钥。
//
// 与日志导出（logs_export.go，§5.5.1）同一套约束：流式写出、不落临时文件；
// 命中行数与时间跨度必须在写出响应体**之前**校验——一旦发出 200 头部，就再也
// 无法用 400 + error.code 告诉调用方"超限了"，只能给出截断文件。
//
// 本文件同时承载三者共用的导出机械（时间窗解析复用 logs_export.go 的
// parseExportWindow / parseExportFormat，错误码与判定顺序因此与 §5.5.1 完全一致），
// channels_export.go / keys_export.go 只声明各自的列与行映射。

var auditExportCSVColumns = []string{
	"ts",
	"actor",
	"action",
	"resource",
	"name",
	"before_digest",
	"after_digest",
	"dry_run",
}

// ExportAudit GET /api/audit/export（由 G5 统一注册路由）。
//
// 参数：format=csv|ndjson（默认 csv）、from/to（RFC3339，必填，≤31 天）、
// action/resource/name（可选，精确匹配）。
func ExportAudit(c *gin.Context) {
	exportAudit(c, exportRowLimit)
}

// exportAudit 是 ExportAudit 的可注入阈值变体：rowLimit 只影响行数上限，
// 生产取值恒为 exportRowLimit；单测用小阈值覆盖 export_too_large 分支。
func exportAudit(c *gin.Context, rowLimit int) {
	format, err := parseExportFormat(c.Query("format"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	// 审计导出必须给时间区间（§5.5.2），复用日志导出的 from/to 校验：
	// 缺参/RFC3339 非法/to<=from 均为 validation_failed，超过 31 天为
	// export_range_too_large。
	from, to, err := parseExportWindow(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	filter := model.PBRAuditFilter{
		Action:   strings.TrimSpace(c.Query("action")),
		Resource: strings.TrimSpace(c.Query("resource")),
		Name:     strings.TrimSpace(c.Query("name")),
		Since:    from,
		Until:    to,
	}

	// 先确定命中行数，再决定是否允许写出（超限必须能在响应体发出前 400）。
	matched, err := model.CountPBRAudits(filter)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if matched > int64(rowLimit) {
		writeAPIError(c, &apiError{
			status:  http.StatusBadRequest,
			code:    exportCodeTooLarge,
			message: fmt.Sprintf("export matched %d rows, exceeding the %d row limit", matched, rowLimit),
			hint:    "narrow from/to or add filters (action/resource/name), then retry",
			details: gin.H{"matched": matched, "limit": rowLimit},
		})
		return
	}

	setAdminExportHeaders(c, "audit", format, from, to)
	sink := newAdminExportSink(c, format, rowLimit)
	rows, streamErr := streamAuditExport(sink, filter)
	if streamErr != nil && !errors.Is(streamErr, errExportLimitReached) {
		// 响应体已经开始流式写出，不能再写 JSON 错误包络；把错误交给 gin 的
		// 上下文错误链（访问日志/中间件可见），调用方看到的是明确截断的流。
		_ = c.Error(streamErr)
	}

	// 导出属读操作，仍留审计（api-spec §5.5.2）：行数 + 过滤摘要，不落导出内容。
	summary := adminExportAuditName([]string{
		"audit",
		format,
		"rows=" + strconv.Itoa(rows),
		"from=" + rfc3339(from),
		"to=" + rfc3339(to),
		auditFilterPart("action", filter.Action),
		auditFilterPart("resource", filter.Resource),
		auditFilterPart("name", filter.Name),
	})
	writeAudit(c, "export", "audit", summary, gin.H{
		"format":   format,
		"rows":     rows,
		"from":     rfc3339(from),
		"to":       rfc3339(to),
		"action":   filter.Action,
		"resource": filter.Resource,
		"name":     filter.Name,
	})
}

// streamAuditExport 写出表头，再按 id 倒序游标分页写出全部命中行，返回实际
// 写出行数。表头与数据行共用同一个 sink，任一步失败都由调用方统一转入
// 上下文错误链（响应体已开始发送，不能再写 JSON 错误包络）。
func streamAuditExport(sink *adminExportSink, filter model.PBRAuditFilter) (int, error) {
	if err := sink.writeCSVHeader(auditExportCSVColumns); err != nil {
		return sink.rows, err
	}
	for {
		pageFilter := filter
		pageFilter.Limit = exportPageSize
		entries, err := model.ListPBRAuditsFiltered(pageFilter)
		if err != nil {
			return sink.rows, err
		}
		if len(entries) == 0 {
			break
		}
		for i := range entries {
			entry := &entries[i]
			if sink.csv != nil {
				if err := sink.writeCSVRow(auditExportCSVRow(entry)); err != nil {
					return sink.rows, err
				}
			} else if err := sink.writeJSONRow(auditExportJSONRow(entry)); err != nil {
				return sink.rows, err
			}
			// 下一页从本页最后一行继续（id desc 的排他游标）。
			filter.BeforeId = entry.Id
		}
		if len(entries) < exportPageSize {
			break
		}
	}
	return sink.rows, sink.flush()
}

// auditExportCSVRow 按 auditExportCSVColumns 的固定顺序生成一行。
// 布尔值用 true/false、时间用 RFC3339、空值输出空串（api-spec §5.5.2）。
func auditExportCSVRow(entry *model.PBRAuditLog) []string {
	return []string{
		rfc3339(entry.Ts),
		entry.Actor,
		entry.Action,
		entry.Resource,
		entry.Name,
		entry.BeforeDigest,
		entry.AfterDigest,
		strconv.FormatBool(entry.DryRun),
	}
}

// auditExportJSONRow 输出审计行全字段（§5.5.2 NDJSON）。
//
// 字段集合与 GET /api/audit 的条目一致（含 id）；ts 同口径为 RFC3339，
// 不把存储层的 Unix 秒直接透出。
func auditExportJSONRow(entry *model.PBRAuditLog) gin.H {
	return gin.H{
		"id":            entry.Id,
		"ts":            rfc3339(entry.Ts),
		"actor":         entry.Actor,
		"action":        entry.Action,
		"resource":      entry.Resource,
		"name":          entry.Name,
		"before_digest": entry.BeforeDigest,
		"after_digest":  entry.AfterDigest,
		"dry_run":       entry.DryRun,
	}
}

// auditFilterPart 生成审计 name 里的过滤摘要片段；空过滤不占位。
func auditFilterPart(key, value string) string {
	if value == "" {
		return ""
	}
	return key + "=" + value
}

// parseAdminCSVFormat 解析渠道/密钥导出的 format。
//
// 契约（§5.5.2）只声明 format=csv；省略即 csv，其余取值一律 400——静默退回
// CSV 会让调用方以为拿到了 NDJSON，解析器在第一条记录才报错。
func parseAdminCSVFormat(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", exportFormatCSV:
		return exportFormatCSV, nil
	default:
		return "", &apiError{
			code:    apierr.CodeValidationFailed,
			message: "format must be csv",
		}
	}
}

// setAdminExportHeaders 按 §5.5.2 设置附件响应头。
// 文件名只由固定资源名与时间戳拼出，不含调用方原文，避免响应头注入。
func setAdminExportHeaders(c *gin.Context, resource, format string, from, to int64) {
	contentType := "text/csv; charset=utf-8"
	ext := "csv"
	if format == exportFormatNDJSON {
		// NDJSON 的内容类型沿用 §5.5.1 的口径（application/x-ndjson）。
		contentType = "application/x-ndjson"
		ext = "ndjson"
	}
	filename := fmt.Sprintf("pbr-%s.%s", resource, ext)
	if from > 0 || to > 0 {
		filename = fmt.Sprintf("pbr-%s-%s-%s.%s", resource,
			exportFilenameTime(from), exportFilenameTime(to), ext)
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
}

// adminExportAuditName 生成导出审计 name 列的可读摘要。
// PBRAuditLog.Name 只有 128 字符，超长截断（与日志导出的 exportAuditName 同口径）。
func adminExportAuditName(parts []string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	name := strings.Join(kept, " ")
	if runes := []rune(name); len(runes) > exportAuditNameRunes {
		name = string(runes[:exportAuditNameRunes])
	}
	return name
}

// adminExportSink 是三个导出共用的流式写出器：CSV 用 encoding/csv 逐行写
// c.Writer，NDJSON 逐行 JSON + 换行；周期性 Flush，不缓冲整份导出。
type adminExportSink struct {
	c     *gin.Context
	csv   *csv.Writer
	rows  int
	limit int
}

func newAdminExportSink(c *gin.Context, format string, limit int) *adminExportSink {
	sink := &adminExportSink{c: c, limit: limit}
	if format == exportFormatCSV {
		sink.csv = csv.NewWriter(c.Writer)
	}
	return sink
}

func (s *adminExportSink) writeCSVHeader(columns []string) error {
	if s.csv == nil {
		return nil
	}
	return s.csv.Write(columns)
}

func (s *adminExportSink) writeCSVRow(values []string) error {
	if s.csv == nil {
		return errors.New("csv row written to a non-csv export")
	}
	if err := s.reserve(); err != nil {
		return err
	}
	if err := s.csv.Write(values); err != nil {
		return err
	}
	return s.advance()
}

func (s *adminExportSink) writeJSONRow(value any) error {
	if err := s.reserve(); err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := s.c.Writer.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return s.advance()
}

// reserve 守住导出上界：count 之后并发新增的行不越界写出。
func (s *adminExportSink) reserve() error {
	if s.limit > 0 && s.rows >= s.limit {
		return errExportLimitReached
	}
	return nil
}

func (s *adminExportSink) advance() error {
	s.rows++
	if s.rows%exportFlushEvery == 0 {
		return s.flush()
	}
	return nil
}

func (s *adminExportSink) flush() error {
	if s.csv != nil {
		s.csv.Flush()
		if err := s.csv.Error(); err != nil {
			return err
		}
	}
	s.c.Writer.Flush()
	return nil
}
