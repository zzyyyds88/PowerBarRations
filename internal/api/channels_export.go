package api

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 渠道导出（api-spec §5.5.2）：全量 CSV，不分页。
//
// **绝不输出 key 明文**：key 只用于派生 key_set / key_prefix 两个布尔/前缀值，
// 行映射不读取原文；渠道规模由单用户配置决定，全量加载后流式写出即可，
// 不设分页也不落临时文件。

var channelExportCSVColumns = []string{
	"name",
	"type",
	"base_url",
	"models",
	"enabled",
	"key_set",
	"key_prefix",
	"proxy",
	"created_at",
	"updated_at",
}

// ExportChannels GET /api/channels/export（由 G5 统一注册路由）。
func ExportChannels(c *gin.Context) {
	format, err := parseAdminCSVFormat(c.Query("format"))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	// 显式列清单是"无明文"的第一道保证：只取构造导出行必需的字段；key 仅用于
	// 派生 key_set / key_prefix，绝不进入导出内容（行映射只读这两个派生值）。
	// setting 为 text（proxy 等渠道设置），models 为逗号分隔串，其余为展示字段。
	var channels []*model.Channel
	err = model.DB.Model(&model.Channel{}).
		Select("name, type, base_url, models, key, status, setting, created_time, updated_at").
		Order("name asc").
		Find(&channels).Error
	if err != nil {
		writeAPIError(c, err)
		return
	}

	setAdminExportHeaders(c, "channels", format, 0, 0)
	// 契约（§5.5.2）要求渠道导出是全量、不分页：不给行数上限，避免在 10 万级
	// 配置下静默截断文件。所有行已在内存里，写出本身不再扩大内存上界。
	sink := newAdminExportSink(c, format, 0)
	rows, writeErr := streamChannelExport(sink, channels)
	if writeErr != nil && !errors.Is(writeErr, errExportLimitReached) {
		_ = c.Error(writeErr)
	}

	// 导出属读操作，仍留审计（api-spec §5.5.2）：action=export、resource=channel。
	summary := adminExportAuditName([]string{
		"channels",
		format,
		"rows=" + strconv.Itoa(rows),
	})
	writeAudit(c, "export", "channel", summary, gin.H{
		"format": format,
		"rows":   rows,
	})
}

// streamChannelExport 先写表头再逐行写出，返回实际写出行数。
func streamChannelExport(sink *adminExportSink, channels []*model.Channel) (int, error) {
	if err := sink.writeCSVHeader(channelExportCSVColumns); err != nil {
		return sink.rows, err
	}
	for _, ch := range channels {
		if err := sink.writeCSVRow(channelExportCSVRow(ch)); err != nil {
			return sink.rows, err
		}
	}
	return sink.rows, sink.flush()
}

// channelExportCSVRow 按 channelExportCSVColumns 的固定顺序生成一行。
//
// 取值口径与 GET /api/channels 的响应一致（type 用 slug、models 是清理后的
// 模型清单、enabled 由 status 推导、时间 RFC3339、空值空串），区别只有
// models 用 `|` 连接、以及不含 param_override/prices 等非导出列。
func channelExportCSVRow(ch *model.Channel) []string {
	return []string{
		ch.Name,
		ChannelTypeSlug(ch.Type),
		ch.GetBaseURL(),
		strings.Join(ch.GetModels(), "|"),
		strconv.FormatBool(ch.Status == common.ChannelStatusEnabled),
		strconv.FormatBool(strings.TrimSpace(ch.Key) != ""),
		channelExportKeyPrefix(ch.Key),
		channelExportProxy(ch),
		rfc3339(ch.CreatedTime),
		rfc3339(ch.UpdatedAt),
	}
}

// channelExportKeyPrefix 与 channelResponse 的 key_prefix 同口径：只回显足以
// 辨认的前缀，短密钥不能整串回显（api-spec §4.1）。
func channelExportKeyPrefix(key string) string {
	key = strings.TrimSpace(key)
	switch {
	case len(key) > 4:
		return key[:4]
	case len(key) > 1:
		return key[:len(key)-1]
	default:
		return ""
	}
}

// channelExportProxy 只读解析渠道设置里的 proxy。
//
// 不走 Channel.GetSetting()：该方法在 JSON 损坏时会顺手 Save 回库，而导出是
// 纯读操作，不能因为"读了一次"产生写入。解析失败按空值导出。
func channelExportProxy(ch *model.Channel) string {
	if ch.Setting == nil || strings.TrimSpace(*ch.Setting) == "" {
		return ""
	}
	var setting struct {
		Proxy string `json:"proxy"`
	}
	if err := json.Unmarshal([]byte(*ch.Setting), &setting); err != nil {
		return ""
	}
	return setting.Proxy
}
