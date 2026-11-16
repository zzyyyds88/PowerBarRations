package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 客户端密钥导出（api-spec §5.5.2）：列顺序、数组 `|` 连接、空值口径、
// 以及**绝不输出 key / key_plain**。

const (
	keyExportPlainAlpha = "sk-client-key-PLAIN-alpha-7c6b5a"
	keyExportPlainBeta  = "sk-client-key-PLAIN-beta-1a2b3c"
)

var wantKeyExportCSVColumns = []string{
	"name", "key_prefix", "enabled", "lane_policy_mode", "allow_lanes",
	"deny_lanes", "ip_allowlist", "rate_limit_rpm", "max_concurrency",
	"expires_at", "notes", "created_at", "updated_at", "last_used_at",
}

func seedExportKeys(t *testing.T, db *gorm.DB) {
	t.Helper()
	expires := adminExportFrom.Unix() + 365*24*60*60
	rows := []*model.ClientKey{
		{
			Name:           "key-alpha",
			KeyHash:        model.HashClientKey(keyExportPlainAlpha),
			KeyPrefix:      "sk-cl",
			KeyPlain:       keyExportPlainAlpha,
			Enabled:        true,
			LanePolicy:     `{"mode":"allow","allow_lanes":["lane-a","lane-b"],"deny_lanes":["lane-c"]}`,
			IPAllowlist:    `["10.0.0.1"," 192.168.1.0/24 "]`,
			RateLimitRPM:   60,
			MaxConcurrency: 4,
			ExpiresAt:      &expires,
			Notes:          "team alpha",
			CreatedAt:      adminExportFrom.Unix(),
			UpdatedAt:      adminExportFrom.Unix() + 60,
			LastUsedAt:     adminExportFrom.Unix() + 120,
		},
		{
			Name:        "key-beta",
			KeyHash:     model.HashClientKey(keyExportPlainBeta),
			KeyPrefix:   "sk-be",
			KeyPlain:    keyExportPlainBeta,
			Enabled:     false,
			CreatedAt:   adminExportFrom.Unix() + 180,
			UpdatedAt:   adminExportFrom.Unix() + 240,
			LastUsedAt:  0,
			Notes:       "",
			LanePolicy:  "",
			IPAllowlist: "",
		},
	}
	for _, row := range rows {
		require.NoError(t, db.Create(row).Error)
	}
}

func TestExportKeysCSVContract(t *testing.T) {
	db := setupAdminExportTestDB(t)
	seedExportKeys(t, db)

	recorder := runAdminExport(t, ExportKeys, "")

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "text/csv; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Equal(t,
		`attachment; filename="pbr-keys.csv"`,
		recorder.Header().Get("Content-Disposition"))

	records := parseExportCSV(t, recorder)
	require.Len(t, records, 3, "表头 + 2 个密钥")
	assert.Equal(t, wantKeyExportCSVColumns, records[0], "CSV 列顺序是稳定契约，不得重排")
	assert.Equal(t, wantKeyExportCSVColumns, keyExportCSVColumns)

	alpha := csvRowByColumn(t, records[0], records[1])
	assert.Equal(t, "key-alpha", alpha["name"])
	assert.Equal(t, "sk-cl", alpha["key_prefix"])
	assert.Equal(t, "true", alpha["enabled"])
	assert.Equal(t, "allow", alpha["lane_policy_mode"])
	assert.Equal(t, "lane-a|lane-b", alpha["allow_lanes"], "数组用 | 连接")
	assert.Equal(t, "lane-c", alpha["deny_lanes"])
	assert.Equal(t, "10.0.0.1|192.168.1.0/24", alpha["ip_allowlist"])
	assert.Equal(t, "60", alpha["rate_limit_rpm"])
	assert.Equal(t, "4", alpha["max_concurrency"])
	assert.Equal(t, "2027-01-01T00:00:00Z", alpha["expires_at"])
	assert.Equal(t, "team alpha", alpha["notes"])
	assert.Equal(t, "2026-01-01T00:00:00Z", alpha["created_at"])
	assert.Equal(t, "2026-01-01T00:01:00Z", alpha["updated_at"])
	assert.Equal(t, "2026-01-01T00:02:00Z", alpha["last_used_at"])

	beta := csvRowByColumn(t, records[0], records[2])
	assert.Equal(t, "key-beta", beta["name"])
	assert.Equal(t, "false", beta["enabled"])
	assert.Equal(t, "all", beta["lane_policy_mode"], "未配置 lane_policy 等价 all")
	assert.Equal(t, "", beta["allow_lanes"], "空数组输出空串")
	assert.Equal(t, "", beta["deny_lanes"])
	assert.Equal(t, "", beta["ip_allowlist"])
	assert.Equal(t, "0", beta["rate_limit_rpm"])
	assert.Equal(t, "0", beta["max_concurrency"])
	assert.Equal(t, "", beta["expires_at"], "无过期时间输出空串")
	assert.Equal(t, "", beta["last_used_at"], "从未使用输出空串")

	// 导出属读操作，仍留审计：action=export、resource=key，name 带行数。
	var audits []model.PBRAuditLog
	require.NoError(t, model.DB.Where("action = ? AND resource = ?", "export", "key").Find(&audits).Error)
	require.Len(t, audits, 1)
	assert.Contains(t, audits[0].Name, "rows=2")
}

func TestExportKeysNeverExposesKeyMaterial(t *testing.T) {
	db := setupAdminExportTestDB(t)
	seedExportKeys(t, db)

	recorder := runAdminExport(t, ExportKeys, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	body := recorder.Body.String()
	for _, plain := range []string{keyExportPlainAlpha, keyExportPlainBeta} {
		assert.NotContains(t, body, plain, "响应体不得包含密钥明文")
		// 哈希同样不是导出字段。
		assert.NotContains(t, body, model.HashClientKey(plain))
	}

	// 逐单元格复核：既没有明文，也没有哈希。
	hashes := map[string]bool{
		model.HashClientKey(keyExportPlainAlpha): true,
		model.HashClientKey(keyExportPlainBeta):  true,
	}
	for _, row := range parseExportCSV(t, recorder) {
		for _, cell := range row {
			assert.NotEqual(t, keyExportPlainAlpha, cell)
			assert.NotEqual(t, keyExportPlainBeta, cell)
			assert.False(t, hashes[cell], "单元格不得是 key_hash")
		}
	}

	// 列名层面也不存在密钥字段：只有 key_prefix。
	header := strings.Join(parseExportCSV(t, recorder)[0], ",")
	for _, forbidden := range []string{"key_hash", "key_plain", "key,"} {
		assert.NotContains(t, header, forbidden)
	}
}

func TestExportKeysEmptyResult(t *testing.T) {
	setupAdminExportTestDB(t)

	recorder := runAdminExport(t, ExportKeys, "")

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	records := parseExportCSV(t, recorder)
	require.Len(t, records, 1, "空表只输出表头")
	assert.Equal(t, wantKeyExportCSVColumns, records[0])

	// 空导出也要留审计（行数 0）。
	var audits []model.PBRAuditLog
	require.NoError(t, model.DB.Where("action = ? AND resource = ?", "export", "key").Find(&audits).Error)
	require.Len(t, audits, 1)
	assert.Contains(t, audits[0].Name, "rows=0")
}

func TestExportKeysRejectsNonCSVFormat(t *testing.T) {
	setupAdminExportTestDB(t)

	recorder := runAdminExport(t, ExportKeys, "format=ndjson")
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
