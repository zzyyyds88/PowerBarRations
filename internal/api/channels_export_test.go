package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 渠道导出（api-spec §5.5.2）：列顺序、取值口径、空表、以及**绝不输出 key 明文**。

const (
	// 渠道夹具里的密钥是唯一串，用于断言响应体不含明文（真实密钥不会被导出）。
	channelExportSecretAlpha = "sk-channel-LIVE-9f8e7d6c5b4a"
	channelExportSecretBeta  = "z9q-channel-beta-secret"
	channelExportSecretShort = "zq7"
)

var wantChannelExportCSVColumns = []string{
	"name", "type", "base_url", "models", "enabled", "key_set",
	"key_prefix", "proxy", "created_at", "updated_at",
}

func adminExportStrPtr(value string) *string { return &value }

func seedExportChannels(t *testing.T, db *gorm.DB) {
	t.Helper()
	setting := `{"proxy":"http://127.0.0.1:8888"}`
	rows := []*model.Channel{
		{
			Name:        "alpha",
			Type:        constant.ChannelTypeOpenAI,
			Key:         channelExportSecretAlpha,
			Status:      common.ChannelStatusEnabled,
			BaseURL:     adminExportStrPtr("https://up-alpha.example.com/custom"),
			Models:      "model-a, model-b,,model-a",
			Setting:     &setting,
			CreatedTime: adminExportFrom.Unix(),
			UpdatedAt:   adminExportFrom.Unix() + 60,
		},
		{
			Name:        "beta",
			Type:        constant.ChannelTypeAnthropic,
			Key:         channelExportSecretBeta,
			Status:      common.ChannelStatusManuallyDisabled,
			BaseURL:     adminExportStrPtr("https://up-beta.example.com/custom"),
			Models:      "model-c",
			CreatedTime: adminExportFrom.Unix() + 120,
			UpdatedAt:   adminExportFrom.Unix() + 180,
		},
		{
			Name:        "gamma",
			Type:        constant.ChannelTypeOpenAI,
			Key:         channelExportSecretShort,
			Status:      common.ChannelStatusEnabled,
			BaseURL:     adminExportStrPtr("https://up-gamma.example.com/custom"),
			Models:      "",
			CreatedTime: adminExportFrom.Unix() + 240,
			UpdatedAt:   adminExportFrom.Unix() + 300,
		},
	}
	for _, row := range rows {
		require.NoError(t, db.Create(row).Error)
	}
}

func TestExportChannelsCSVContract(t *testing.T) {
	db := setupAdminExportTestDB(t)
	seedExportChannels(t, db)

	recorder := runAdminExport(t, ExportChannels, "")

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "text/csv; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Equal(t,
		`attachment; filename="pbr-channels.csv"`,
		recorder.Header().Get("Content-Disposition"))

	records := parseExportCSV(t, recorder)
	require.Len(t, records, 4, "表头 + 3 个渠道")
	assert.Equal(t, wantChannelExportCSVColumns, records[0], "CSV 列顺序是稳定契约，不得重排")
	assert.Equal(t, wantChannelExportCSVColumns, channelExportCSVColumns)

	alpha := csvRowByColumn(t, records[0], records[1])
	assert.Equal(t, "alpha", alpha["name"])
	assert.Equal(t, "openai", alpha["type"], "type 用对外 slug")
	assert.Equal(t, "https://up-alpha.example.com/custom", alpha["base_url"])
	assert.Equal(t, "model-a|model-b", alpha["models"], "models 用 | 连接并去重去空")
	assert.Equal(t, "true", alpha["enabled"])
	assert.Equal(t, "true", alpha["key_set"])
	assert.Equal(t, "sk-c", alpha["key_prefix"], "key_prefix 只回显前 4 个字符")
	assert.Equal(t, "http://127.0.0.1:8888", alpha["proxy"])
	assert.Equal(t, "2026-01-01T00:00:00Z", alpha["created_at"])
	assert.Equal(t, "2026-01-01T00:01:00Z", alpha["updated_at"])

	beta := csvRowByColumn(t, records[0], records[2])
	assert.Equal(t, "beta", beta["name"])
	assert.Equal(t, "anthropic", beta["type"])
	assert.Equal(t, "false", beta["enabled"], "手工停用渠道导出 enabled=false")
	assert.Equal(t, "z9q-", beta["key_prefix"])
	assert.Equal(t, "", beta["proxy"], "无 proxy 输出空串")

	gamma := csvRowByColumn(t, records[0], records[3])
	assert.Equal(t, "", gamma["models"], "无声明模型输出空串")
	assert.Equal(t, "zq", gamma["key_prefix"], "短密钥不能整串回显（api-spec §4.1）")

	// 导出属读操作，仍留审计：action=export、resource=channel，name 带行数。
	var audits []model.PBRAuditLog
	require.NoError(t, model.DB.Where("action = ? AND resource = ?", "export", "channel").Find(&audits).Error)
	require.Len(t, audits, 1)
	assert.Contains(t, audits[0].Name, "rows=3")
}

func TestExportChannelsNeverExposesKeyMaterial(t *testing.T) {
	db := setupAdminExportTestDB(t)
	seedExportChannels(t, db)

	recorder := runAdminExport(t, ExportChannels, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	body := recorder.Body.String()
	for _, secret := range []string{
		channelExportSecretAlpha,
		channelExportSecretBeta,
		channelExportSecretShort,
	} {
		assert.NotContains(t, body, secret, "响应体不得包含渠道密钥明文")
	}
	// 任何单元格都不能等于短密钥原文（短密钥的前缀规则会截断，但整串本身绝不出现）。
	for _, row := range parseExportCSV(t, recorder) {
		for _, cell := range row {
			assert.NotEqual(t, channelExportSecretShort, cell)
		}
	}

	// 列名层面也不存在密钥字段：只有派生的 key_set / key_prefix。
	header := strings.Join(parseExportCSV(t, recorder)[0], ",")
	for _, forbidden := range []string{"key_hash", "key_plain", "key,"} {
		assert.NotContains(t, header, forbidden)
	}
	assert.NotContains(t, header, "openai_organization")
}

func TestExportChannelsEmptyResult(t *testing.T) {
	setupAdminExportTestDB(t)

	recorder := runAdminExport(t, ExportChannels, "")

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	records := parseExportCSV(t, recorder)
	require.Len(t, records, 1, "空表只输出表头")
	assert.Equal(t, wantChannelExportCSVColumns, records[0])

	// 空导出也要留审计（行数 0）。
	var audits []model.PBRAuditLog
	require.NoError(t, model.DB.Where("action = ? AND resource = ?", "export", "channel").Find(&audits).Error)
	require.Len(t, audits, 1)
	assert.Contains(t, audits[0].Name, "rows=0")
}

func TestExportChannelsRejectsNonCSVFormat(t *testing.T) {
	setupAdminExportTestDB(t)

	for _, query := range []string{"format=ndjson", "format=json"} {
		recorder := runAdminExport(t, ExportChannels, query)
		require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
		assert.Equal(t, "validation_failed", body.Error.Code)
		assert.Empty(t, recorder.Header().Get("Content-Disposition"), "校验失败不得设置附件头")
	}

	// 省略 format 与显式 format=csv 等价。
	ok := runAdminExport(t, ExportChannels, "format=csv")
	require.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
	assert.NotEmpty(t, ok.Header().Get("Content-Disposition"))
}

// 时间字段口径：空 created/updated 输出空串而不是 1970。
func TestExportChannelsEmptyTimestamps(t *testing.T) {
	db := setupAdminExportTestDB(t)
	require.NoError(t, db.Create(&model.Channel{
		Name:    "no-time",
		Type:    constant.ChannelTypeOpenAI,
		Key:     "sk-time-secret",
		Status:  common.ChannelStatusEnabled,
		BaseURL: adminExportStrPtr("https://up.example.com/custom"),
	}).Error)
	// GORM 的 UpdatedAt 是内建约定字段，Create 时会写入当前时间；用 UpdateColumns
	// 归零后验证导出对"无时间"的输出口径（不能把 1970 当空值）。
	require.NoError(t, db.Model(&model.Channel{}).Where("name = ?", "no-time").
		UpdateColumns(map[string]any{"created_time": 0, "updated_at": 0}).Error)

	recorder := runAdminExport(t, ExportChannels, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	records := parseExportCSV(t, recorder)
	require.Len(t, records, 2)
	row := csvRowByColumn(t, records[0], records[1])
	assert.Equal(t, "", row["created_at"])
	assert.Equal(t, "", row["updated_at"])
	assert.Equal(t, "sk-t", row["key_prefix"])
}
