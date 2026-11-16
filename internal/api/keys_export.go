package api

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 客户端密钥导出（api-spec §5.5.2）：全量 CSV，不分页。
//
// **绝不输出 key / key_plain**：查询显式排除 key_hash 与 key_plain，只导出
// key_prefix 与策略字段。数组（allow_lanes / deny_lanes / ip_allowlist）用 `|`
// 连接，空数组输出空串。

var keyExportCSVColumns = []string{
	"name",
	"key_prefix",
	"enabled",
	"lane_policy_mode",
	"allow_lanes",
	"deny_lanes",
	"ip_allowlist",
	"rate_limit_rpm",
	"max_concurrency",
	"expires_at",
	"notes",
	"created_at",
	"updated_at",
	"last_used_at",
}

// ExportKeys GET /api/keys/export（由 G5 统一注册路由）。
func ExportKeys(c *gin.Context) {
	format, err := parseAdminCSVFormat(c.Query("format"))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	// 显式列清单是"无明文"的第一道保证：key_hash / key_plain 根本不出库。
	var keys []model.ClientKey
	err = model.DB.Model(&model.ClientKey{}).
		Select("name, key_prefix, enabled, lane_policy, ip_allowlist, rate_limit_rpm, " +
			"max_concurrency, expires_at, notes, created_at, updated_at, last_used_at").
		Order("name asc").
		Find(&keys).Error
	if err != nil {
		writeAPIError(c, err)
		return
	}

	setAdminExportHeaders(c, "keys", format, 0, 0)
	// 契约（§5.5.2）要求密钥导出是全量、不分页：不给行数上限，避免在 10 万级
	// 配置下静默截断文件。所有行已在内存里，写出本身不再扩大内存上界。
	sink := newAdminExportSink(c, format, 0)
	rows, writeErr := streamKeyExport(sink, keys)
	if writeErr != nil && !errors.Is(writeErr, errExportLimitReached) {
		_ = c.Error(writeErr)
	}

	// 导出属读操作，仍留审计（api-spec §5.5.2）：action=export、resource=key。
	summary := adminExportAuditName([]string{
		"keys",
		format,
		"rows=" + strconv.Itoa(rows),
	})
	writeAudit(c, "export", "key", summary, gin.H{
		"format": format,
		"rows":   rows,
	})
}

// streamKeyExport 先写表头再逐行写出，返回实际写出行数。
func streamKeyExport(sink *adminExportSink, keys []model.ClientKey) (int, error) {
	if err := sink.writeCSVHeader(keyExportCSVColumns); err != nil {
		return sink.rows, err
	}
	for i := range keys {
		if err := sink.writeCSVRow(keyExportCSVRow(&keys[i])); err != nil {
			return sink.rows, err
		}
	}
	return sink.rows, sink.flush()
}

// keyExportCSVRow 按 keyExportCSVColumns 的固定顺序生成一行。
//
// 取值口径与 GET /api/keys 的响应一致（lane_policy 解析后拆三列、时间 RFC3339、
// 空值空串）；区别只是数组用 `|` 连接，且不含任何密钥材料。
func keyExportCSVRow(key *model.ClientKey) []string {
	policy := model.ParseLanePolicy(key.LanePolicy)
	return []string{
		key.Name,
		key.KeyPrefix,
		strconv.FormatBool(key.Enabled),
		policy.Mode,
		strings.Join(policy.AllowLanes, "|"),
		strings.Join(policy.DenyLanes, "|"),
		strings.Join(keyExportIPAllowlist(key.IPAllowlist), "|"),
		strconv.Itoa(key.RateLimitRPM),
		strconv.Itoa(key.MaxConcurrency),
		keyExportExpiresAt(key),
		key.Notes,
		rfc3339(key.CreatedAt),
		rfc3339(key.UpdatedAt),
		rfc3339(key.LastUsedAt),
	}
}

// keyExportIPAllowlist 只读解析 ip_allowlist JSON 数组（空/损坏按空表导出）。
func keyExportIPAllowlist(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	cleaned := make([]string, 0, len(list))
	for _, item := range list {
		if item = strings.TrimSpace(item); item != "" {
			cleaned = append(cleaned, item)
		}
	}
	return cleaned
}

// keyExportExpiresAt：未设置或非正数视为无过期时间，输出空串。
func keyExportExpiresAt(key *model.ClientKey) string {
	if key.ExpiresAt == nil || *key.ExpiresAt <= 0 {
		return ""
	}
	return rfc3339(*key.ExpiresAt)
}
