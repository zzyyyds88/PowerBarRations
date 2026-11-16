package api

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 管理面导出（api-spec §5.5.2）的契约测试：审计 / 渠道 / 密钥三种导出的
// 列顺序、时间与布尔格式、过滤、区间与行数限制、审计落库、以及**无密钥明文**。
//
// 三个导出共用本文件的测试库与调用辅助。

var (
	adminExportFrom = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	adminExportTo   = adminExportFrom.Add(24 * time.Hour)
)

func setupAdminExportTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "admin-export-test.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	// 导出会写一条审计，PBRAuditLog 必须一起迁移，否则审计断言拿到的是空表。
	require.NoError(t, db.AutoMigrate(&model.PBRAuditLog{}, &model.Channel{}, &model.ClientKey{}))
	previous := model.DB
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() { model.DB = previous })
	return db
}

// runAdminExport 直接调用 handler（与 logs_export_test.go 同样的轻量入口）。
func runAdminExport(t *testing.T, handler gin.HandlerFunc, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/export?"+rawQuery, nil)
	handler(c)
	return recorder
}

func adminExportWindowQuery(extra url.Values) string {
	values := url.Values{}
	values.Set("from", adminExportFrom.Format(time.RFC3339))
	values.Set("to", adminExportTo.Format(time.RFC3339))
	for key, items := range extra {
		for _, item := range items {
			values.Add(key, item)
		}
	}
	return values.Encode()
}

func parseExportCSV(t *testing.T, recorder *httptest.ResponseRecorder) [][]string {
	t.Helper()
	records, err := csv.NewReader(strings.NewReader(recorder.Body.String())).ReadAll()
	require.NoError(t, err)
	return records
}

// csvRowByColumn 返回"列名 → 值"的索引与单行取值，供逐列断言。
func csvRowByColumn(t *testing.T, header, row []string) map[string]string {
	t.Helper()
	require.Len(t, row, len(header), "行列数必须与表头一致")
	values := make(map[string]string, len(header))
	for i, column := range header {
		values[column] = row[i]
	}
	return values
}

var wantAuditExportCSVColumns = []string{
	"ts", "actor", "action", "resource", "name",
	"before_digest", "after_digest", "dry_run",
}

func seedAuditExportRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	rows := []model.PBRAuditLog{
		{
			Ts:           adminExportFrom.Unix() + 60,
			Actor:        "admin",
			Action:       "create",
			Resource:     "channel",
			Name:         "ch-a",
			BeforeDigest: "",
			AfterDigest:  strings.Repeat("a", 64),
		},
		{
			Ts:           adminExportFrom.Unix() + 120,
			Actor:        "admin",
			Action:       "update",
			Resource:     "client_key",
			Name:         "key-a",
			BeforeDigest: strings.Repeat("b", 64),
			AfterDigest:  strings.Repeat("c", 64),
			DryRun:       true,
		},
		{
			Ts:          adminExportFrom.Unix() + 180,
			Actor:       "admin",
			Action:      "reset",
			Resource:    "lane",
			Name:        "rows=3",
			AfterDigest: strings.Repeat("d", 64),
		},
		// 区间外：区间过滤必须把它排除。
		{
			Ts:          adminExportFrom.Add(40 * 24 * time.Hour).Unix(),
			Actor:       "admin",
			Action:      "create",
			Resource:    "channel",
			Name:        "ch-outside",
			AfterDigest: strings.Repeat("e", 64),
		},
	}
	for i := range rows {
		require.NoError(t, db.Create(&rows[i]).Error)
	}
}

func TestExportAuditCSVContract(t *testing.T) {
	db := setupAdminExportTestDB(t)
	seedAuditExportRows(t, db)

	recorder := runAdminExport(t, ExportAudit, adminExportWindowQuery(nil))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "text/csv; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Equal(t,
		`attachment; filename="pbr-audit-20260101T000000Z-20260102T000000Z.csv"`,
		recorder.Header().Get("Content-Disposition"))
	assert.True(t, recorder.Flushed, "导出必须流式 Flush，而不是攒完再写")

	records := parseExportCSV(t, recorder)
	require.Len(t, records, 4, "表头 + 区间内 3 行")
	assert.Equal(t, wantAuditExportCSVColumns, records[0], "CSV 列顺序是稳定契约，不得重排")
	assert.Equal(t, wantAuditExportCSVColumns, auditExportCSVColumns)
	for i, row := range records {
		assert.Len(t, row, len(wantAuditExportCSVColumns), "第 %d 行列数不一致", i)
	}

	// id 倒序：最新的 reset 行在前，其次 update，最后 create；区间外的 ch-outside 不出现。
	latest := csvRowByColumn(t, records[0], records[1])
	assert.Equal(t, "2026-01-01T00:03:00Z", latest["ts"], "ts 必须是 RFC3339，不能是 Unix 秒")
	assert.Equal(t, "admin", latest["actor"])
	assert.Equal(t, "reset", latest["action"])
	assert.Equal(t, "lane", latest["resource"])
	assert.Equal(t, "rows=3", latest["name"])

	update := csvRowByColumn(t, records[0], records[2])
	assert.Equal(t, "update", update["action"])
	assert.Equal(t, "true", update["dry_run"], "布尔输出 true/false")
	assert.Equal(t, strings.Repeat("b", 64), update["before_digest"])
	assert.Equal(t, strings.Repeat("c", 64), update["after_digest"])

	create := csvRowByColumn(t, records[0], records[3])
	assert.Equal(t, "create", create["action"])
	assert.Equal(t, "false", create["dry_run"])
	assert.Equal(t, "", create["before_digest"], "空值输出空串")

	assert.NotContains(t, recorder.Body.String(), "ch-outside", "区间外的审计行不得导出")

	// 导出本身必须落一条审计：action=export、resource=audit、name 带行数。
	var audits []model.PBRAuditLog
	require.NoError(t, model.DB.Where("action = ? AND resource = ?", "export", "audit").Find(&audits).Error)
	require.Len(t, audits, 1)
	assert.Contains(t, audits[0].Name, "rows=3")
	assert.Contains(t, audits[0].Name, "from=2026-01-01T00:00:00Z")
	assert.Len(t, audits[0].AfterDigest, 64, "after_digest 只落摘要，不落导出内容")
}

func TestExportAuditNDJSONContract(t *testing.T) {
	db := setupAdminExportTestDB(t)
	seedAuditExportRows(t, db)

	recorder := runAdminExport(t, ExportAudit,
		adminExportWindowQuery(url.Values{"format": {"ndjson"}}))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "application/x-ndjson", recorder.Header().Get("Content-Type"))
	assert.Equal(t,
		`attachment; filename="pbr-audit-20260101T000000Z-20260102T000000Z.ndjson"`,
		recorder.Header().Get("Content-Disposition"))

	lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
	require.Len(t, lines, 3, "每行一条审计记录")
	wantFields := []string{
		"id", "ts", "actor", "action", "resource", "name",
		"before_digest", "after_digest", "dry_run",
	}
	var latest map[string]any
	for i, line := range lines {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry), "第 %d 行必须是完整 JSON 对象", i)
		for _, field := range wantFields {
			assert.Contains(t, entry, field, "NDJSON 是审计行全字段")
		}
		rawTs, ok := entry["ts"].(string)
		require.True(t, ok)
		_, err := time.Parse(time.RFC3339, rawTs)
		assert.NoError(t, err, "ts 必须是 RFC3339")
		if i == 0 {
			latest = entry
		}
	}
	assert.Equal(t, "reset", latest["action"])
	assert.Equal(t, "rows=3", latest["name"])
	assert.Equal(t, false, latest["dry_run"])
}

func TestExportAuditValidation(t *testing.T) {
	setupAdminExportTestDB(t)

	from := url.QueryEscape(adminExportFrom.Format(time.RFC3339))
	to := url.QueryEscape(adminExportTo.Format(time.RFC3339))
	thirtyOneDays := adminExportFrom.Add(31 * 24 * time.Hour)
	thirtyTwoDays := adminExportFrom.Add(32 * 24 * time.Hour)

	cases := []struct {
		name     string
		query    string
		wantCode string
	}{
		{name: "missing from/to", query: "format=csv", wantCode: "validation_failed"},
		{name: "invalid format", query: "format=json&from=" + from + "&to=" + to, wantCode: "validation_failed"},
		{name: "to equal from", query: "from=" + from + "&to=" + from, wantCode: "validation_failed"},
		{name: "unix seconds rejected", query: "from=1767225600&to=1767312000", wantCode: "validation_failed"},
		{
			name:     "range over 31 days",
			query:    "from=" + from + "&to=" + url.QueryEscape(thirtyTwoDays.Format(time.RFC3339)),
			wantCode: "export_range_too_large",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := runAdminExport(t, ExportAudit, tc.query)
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			assert.Equal(t, tc.wantCode, body.Error.Code)
			assert.Empty(t, recorder.Header().Get("Content-Disposition"), "校验失败不得设置附件头")
		})
	}

	// 边界：恰好 31 天允许导出。
	ok := runAdminExport(t, ExportAudit,
		"from="+from+"&to="+url.QueryEscape(thirtyOneDays.Format(time.RFC3339)))
	require.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
}

func TestExportAuditFilters(t *testing.T) {
	db := setupAdminExportTestDB(t)
	seedAuditExportRows(t, db)

	cases := []struct {
		name     string
		filters  url.Values
		wantName []string
	}{
		{name: "action", filters: url.Values{"action": {"create"}}, wantName: []string{"ch-a"}},
		{name: "resource", filters: url.Values{"resource": {"client_key"}}, wantName: []string{"key-a"}},
		{name: "name", filters: url.Values{"name": {"rows=3"}}, wantName: []string{"rows=3"}},
		{
			name:     "combined",
			filters:  url.Values{"action": {"update"}, "resource": {"client_key"}, "name": {"key-a"}},
			wantName: []string{"key-a"},
		},
		{name: "no match", filters: url.Values{"action": {"rotate"}}, wantName: []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := runAdminExport(t, ExportAudit, adminExportWindowQuery(tc.filters))
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			records := parseExportCSV(t, recorder)
			require.NotEmpty(t, records)
			assert.Equal(t, "name", records[0][4], "name 固定是第 5 列")
			got := make([]string, 0, len(records)-1)
			for _, row := range records[1:] {
				got = append(got, row[4])
			}
			assert.Equal(t, tc.wantName, got)
		})
	}

	// 区间过滤：from+130s..from+200s 只含 export 行。
	window := url.Values{}
	window.Set("from", adminExportFrom.Add(130*time.Second).Format(time.RFC3339))
	window.Set("to", adminExportFrom.Add(200*time.Second).Format(time.RFC3339))
	narrowed := runAdminExport(t, ExportAudit, window.Encode())
	require.Equal(t, http.StatusOK, narrowed.Code, narrowed.Body.String())
	records := parseExportCSV(t, narrowed)
	require.Len(t, records, 2)
	assert.Equal(t, "rows=3", records[1][4])
}

func TestExportAuditRowLimit(t *testing.T) {
	db := setupAdminExportTestDB(t)
	seedAuditExportRows(t, db)

	// 区间内 3 行，阈值 2 → 400 export_too_large（必须在写出响应体之前判定）。
	runWithLimit := func(limit int) *httptest.ResponseRecorder {
		gin.SetMode(gin.TestMode)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet,
			"/api/v1/audit/export?"+adminExportWindowQuery(nil), nil)
		exportAudit(c, limit)
		return recorder
	}

	over := runWithLimit(2)
	require.Equal(t, http.StatusBadRequest, over.Code, over.Body.String())
	var body struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Hint    string         `json:"hint"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(over.Body.Bytes(), &body))
	assert.Equal(t, "export_too_large", body.Error.Code)
	assert.EqualValues(t, 3, body.Error.Details["matched"], "body 必须给出命中数")
	assert.EqualValues(t, 2, body.Error.Details["limit"])
	assert.NotEmpty(t, body.Error.Hint)
	assert.Empty(t, over.Header().Get("Content-Disposition"), "超限不得开始附件流")

	// 恰好等于上限允许导出（区间内 3 行）。
	ok := runWithLimit(3)
	require.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
	records := parseExportCSV(t, ok)
	assert.Len(t, records, 4)
}

func TestExportAuditEmptyResult(t *testing.T) {
	setupAdminExportTestDB(t)

	recorder := runAdminExport(t, ExportAudit, adminExportWindowQuery(nil))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	records := parseExportCSV(t, recorder)
	require.Len(t, records, 1, "空结果只输出表头")
	assert.Equal(t, wantAuditExportCSVColumns, records[0])

	// 空导出也要留审计（行数 0）。
	var audits []model.PBRAuditLog
	require.NoError(t, model.DB.Where("action = ? AND resource = ?", "export", "audit").Find(&audits).Error)
	require.Len(t, audits, 1)
	assert.Contains(t, audits[0].Name, "rows=0")
}
