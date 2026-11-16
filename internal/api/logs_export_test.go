package api

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
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

// 日志导出（api-spec §5.5.1）的契约测试：列顺序、NDJSON 可解析、区间/行数限制、
// 空结果、过滤参数透传与审计落库。

var (
	exportTestFrom = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	exportTestTo   = exportTestFrom.Add(24 * time.Hour)
)

func setupLogsExportTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logs-export-test.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	// 导出会写一条审计，PBRAuditLog 必须一起迁移，否则审计断言拿到的是空表。
	require.NoError(t, db.AutoMigrate(&model.PBRRequestLog{}, &model.PBRRequestContext{}, &model.PBRAuditLog{}))
	previous := model.DB
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() { model.DB = previous })
	return db
}

// exportTestQuery 用默认区间拼查询串：调用方只追加过滤参数。
func exportTestQuery(extra url.Values) string {
	values := url.Values{}
	values.Set("from", exportTestFrom.Format(time.RFC3339))
	values.Set("to", exportTestTo.Format(time.RFC3339))
	for key, items := range extra {
		for _, item := range items {
			values.Add(key, item)
		}
	}
	return values.Encode()
}

func runExportLogs(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	return runExportLogsWithLimit(t, query, 0)
}

// runExportLogsWithLimit：limit<=0 走生产入口 ExportLogs；否则走可注入阈值变体。
func runExportLogsWithLimit(t *testing.T, query string, limit int) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/logs/export?"+query, nil)
	if limit <= 0 {
		ExportLogs(c)
	} else {
		exportLogs(c, limit)
	}
	return recorder
}

// seedExportLogs 落三条口径不同的明细行（成功 / 失败 / 另一车道）。
func seedExportLogs(t *testing.T, db *gorm.DB) {
	t.Helper()
	rows := []model.PBRRequestLog{
		{
			Ts:                exportTestFrom.Unix() + 3600,
			LaneName:          "lane-a",
			RequestModel:      "model-a",
			RouteSource:       "explicit",
			MemberChannelName: "ch-a",
			UpstreamModel:     "up-a",
			TokenName:         "key-a",
			Type:              model.LogTypeConsume,
			InboundFormat:     "openai",
			Success:           true,
			HTTPStatus:        200,
			PromptTokens:      1200,
			CompletionTokens:  340,
			TTFTMs:            420,
			TotalMs:           3100,
			IsStream:          true,
			TotalAttempts:     2,
			EstimatedCost:     0.02,
			PriceBand:         -1,
			Attempts:          `[{"attempt_num":1,"member":"ch-a/up-a","status":"success","duration_ms":100}]`,
		},
		{
			Ts:                exportTestFrom.Unix() + 7200,
			LaneName:          "lane-b",
			RequestModel:      "model-b",
			RouteSource:       "explicit",
			MemberChannelName: "ch-b",
			UpstreamModel:     "up-b",
			TokenName:         "key-b",
			Type:              model.LogTypeError,
			InboundFormat:     "anthropic",
			Success:           false,
			HTTPStatus:        429,
			ErrorKind:         "soft_rate_limit",
			ErrorSummary:      "rate limited by upstream",
			TotalMs:           900,
			TotalAttempts:     1,
			EstimatedCost:     0,
			PriceBand:         -1,
		},
		{
			Ts:                exportTestTo.Unix() - 60,
			LaneName:          "lane-a",
			RequestModel:      "model-c",
			RouteSource:       "test",
			MemberChannelName: "ch-a",
			UpstreamModel:     "up-c",
			TokenName:         "key-a",
			Type:              model.LogTypeConsume,
			InboundFormat:     "gemini",
			Success:           true,
			HTTPStatus:        200,
			TotalMs:           1200,
			TotalAttempts:     1,
			PriceBand:         -1,
		},
	}
	for i := range rows {
		require.NoError(t, db.Create(&rows[i]).Error)
	}
}

// wantExportCSVColumns 是 api-spec §5.5.1 的固定列顺序，逐字抄写用于防重排。
var wantExportCSVColumns = []string{
	"ts", "request_id", "lane", "request_model", "route_source", "channel",
	"upstream_model", "key_name", "type", "inbound_format", "success",
	"http_status", "error_kind", "prompt_tokens", "completion_tokens",
	"cache_read_tokens", "cache_write_tokens", "reasoning_tokens", "ttft_ms",
	"total_ms", "is_stream", "total_attempts", "estimated_cost", "price_band",
	"session_id", "session_key", "client", "subagent", "parent_subagent",
	"parent_session_id", "context_tokens", "context_window", "context_counted",
	"context_status", "masked_count", "mask_blocked", "mask_status", "mask_took_ms",
}

func TestExportLogsCSVContract(t *testing.T) {
	db := setupLogsExportTestDB(t)
	seedExportLogs(t, db)

	recorder := runExportLogs(t, exportTestQuery(nil))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "text/csv; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Equal(t,
		`attachment; filename="pbr-logs-20260101T000000Z-20260102T000000Z.csv"`,
		recorder.Header().Get("Content-Disposition"))
	assert.True(t, recorder.Flushed, "导出必须流式 Flush，而不是攒完再写")

	records, err := csv.NewReader(strings.NewReader(recorder.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 4, "表头 + 3 行")
	assert.Equal(t, wantExportCSVColumns, records[0], "CSV 列顺序是稳定契约，不得重排")
	assert.Equal(t, wantExportCSVColumns, exportCSVColumns)
	for i, row := range records {
		assert.Len(t, row, len(wantExportCSVColumns), "第 %d 行列数不一致", i)
	}

	// CSV 只含扁平列：不含 attempts / error_summary（api-spec §5.5.1）。
	body := recorder.Body.String()
	assert.NotContains(t, body, "attempt_num")
	assert.NotContains(t, body, "error_summary")
	assert.NotContains(t, body, "rate limited by upstream")

	// 定位成功行并逐列核对取值（ts 为 RFC3339、布尔为 true/false、成本不带指数）。
	index := map[string]int{}
	for i, column := range records[0] {
		index[column] = i
	}
	var successRow []string
	for _, row := range records[1:] {
		if row[index["lane"]] == "lane-a" && row[index["request_model"]] == "model-a" {
			successRow = row
			break
		}
	}
	require.NotNil(t, successRow, "应导出 lane-a/model-a 行")
	expected := map[string]string{
		"ts":                "2026-01-01T01:00:00Z",
		"request_id":        "1",
		"lane":              "lane-a",
		"request_model":     "model-a",
		"route_source":      "explicit",
		"channel":           "ch-a",
		"upstream_model":    "up-a",
		"key_name":          "key-a",
		"type":              strconv.Itoa(model.LogTypeConsume),
		"inbound_format":    "openai",
		"success":           "true",
		"http_status":       "200",
		"error_kind":        "",
		"prompt_tokens":     "1200",
		"completion_tokens": "340",
		"ttft_ms":           "420",
		"total_ms":          "3100",
		"is_stream":         "true",
		"total_attempts":    "2",
		"estimated_cost":    "0.02",
		// G1 合入 price_band 前，旧单层价格按契约输出 -1（未命中 band）。
		"price_band": "-1",
	}
	for column, want := range expected {
		assert.Equal(t, want, successRow[index[column]], "列 %s 取值不符", column)
	}
}

func TestExportLogsNDJSONContract(t *testing.T) {
	db := setupLogsExportTestDB(t)
	seedExportLogs(t, db)

	recorder := runExportLogs(t, exportTestQuery(url.Values{"format": {"ndjson"}}))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "application/x-ndjson", recorder.Header().Get("Content-Type"))
	assert.Equal(t,
		`attachment; filename="pbr-logs-20260101T000000Z-20260102T000000Z.ndjson"`,
		recorder.Header().Get("Content-Disposition"))

	lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
	require.Len(t, lines, 3, "每行一条 RequestLog")
	var failure map[string]any
	for i, line := range lines {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry), "第 %d 行必须是完整 JSON 对象", i)
		assert.Contains(t, entry, "attempts", "NDJSON 必须含 attempts")
		assert.IsType(t, []any{}, entry["attempts"])
		assert.Equal(t, float64(-1), entry["price_band"], "完整行必须含 price_band")
		// ts 必须是 RFC3339（api-spec §2.5），不能漏成 Unix 秒。
		rawTs, ok := entry["ts"].(string)
		require.True(t, ok)
		_, err := time.Parse(time.RFC3339, rawTs)
		assert.NoError(t, err)
		if entry["lane"] == "lane-b" {
			failure = entry
		}
	}
	require.NotNil(t, failure, "应导出失败行")
	assert.Equal(t, "rate limited by upstream", failure["error_summary"],
		"NDJSON 是完整行，必须保留 error_summary")

	// 成功行的 attempts 是完整数组（含 attempt_num 等），与 CSV 的"不含 attempts"相反。
	assert.Contains(t, recorder.Body.String(), `"attempt_num"`)
}

func TestExportLogsValidationAndRange(t *testing.T) {
	setupLogsExportTestDB(t)

	from := url.QueryEscape(exportTestFrom.Format(time.RFC3339))
	thirtyOneDays := exportTestFrom.Add(31 * 24 * time.Hour)
	thirtyTwoDays := exportTestFrom.Add(32 * 24 * time.Hour)

	cases := []struct {
		name     string
		query    string
		wantCode string
	}{
		{
			name:     "missing from/to",
			query:    "format=csv",
			wantCode: "validation_failed",
		},
		{
			name:     "invalid format",
			query:    "format=json&from=" + from + "&to=" + url.QueryEscape(exportTestTo.Format(time.RFC3339)),
			wantCode: "validation_failed",
		},
		{
			name:     "to before from",
			query:    "from=" + from + "&to=" + from,
			wantCode: "validation_failed",
		},
		{
			name:     "unix seconds are not accepted",
			query:    "from=1767225600&to=1767312000",
			wantCode: "validation_failed",
		},
		{
			name:     "range over 31 days",
			query:    "from=" + from + "&to=" + url.QueryEscape(thirtyTwoDays.Format(time.RFC3339)),
			wantCode: "export_range_too_large",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := runExportLogs(t, tc.query)
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			var body struct {
				Error struct {
					Code    string         `json:"code"`
					Details map[string]any `json:"details"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			assert.Equal(t, tc.wantCode, body.Error.Code)
			assert.Empty(t, recorder.Header().Get("Content-Disposition"), "校验失败不得设置附件头")
		})
	}

	// 边界：恰好 31 天允许导出。
	recorder := runExportLogs(t, "from="+from+"&to="+url.QueryEscape(thirtyOneDays.Format(time.RFC3339)))
	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var body struct {
		Error struct {
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	over := runExportLogs(t, "from="+from+"&to="+url.QueryEscape(thirtyTwoDays.Format(time.RFC3339)))
	require.NoError(t, json.Unmarshal(over.Body.Bytes(), &body))
	assert.EqualValues(t, 31, body.Error.Details["max_days"])
}

// 行数上限用可注入阈值变体覆盖（生产阈值 100,000；不为此插入十万行）。
func TestExportLogsRowLimit(t *testing.T) {
	db := setupLogsExportTestDB(t)
	seedExportLogs(t, db)

	over := runExportLogsWithLimit(t, exportTestQuery(nil), 2)
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
	assert.Contains(t, body.Error.Message, "3")
	assert.NotEmpty(t, body.Error.Hint)
	assert.Empty(t, over.Header().Get("Content-Disposition"), "超限不得开始附件流")

	// 恰好等于上限允许导出。
	ok := runExportLogsWithLimit(t, exportTestQuery(nil), 3)
	require.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
	records, err := csv.NewReader(strings.NewReader(ok.Body.String())).ReadAll()
	require.NoError(t, err)
	assert.Len(t, records, 4)
}

func TestExportLogsEmptyResult(t *testing.T) {
	setupLogsExportTestDB(t)

	csvRecorder := runExportLogs(t, exportTestQuery(nil))
	require.Equal(t, http.StatusOK, csvRecorder.Code, csvRecorder.Body.String())
	records, err := csv.NewReader(strings.NewReader(csvRecorder.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 1, "空结果只输出表头")
	assert.Equal(t, wantExportCSVColumns, records[0])

	ndjsonRecorder := runExportLogs(t, exportTestQuery(url.Values{"format": {"ndjson"}}))
	require.Equal(t, http.StatusOK, ndjsonRecorder.Code, ndjsonRecorder.Body.String())
	assert.Empty(t, strings.TrimSpace(ndjsonRecorder.Body.String()))

	// 空导出也要留审计（行数 0）。
	var audits []model.PBRAuditLog
	require.NoError(t, model.DB.Where("action = ?", "export").Find(&audits).Error)
	require.NotEmpty(t, audits)
	assert.Contains(t, audits[0].Name, "rows=0")
}

func TestExportLogsFilterPassthrough(t *testing.T) {
	db := setupLogsExportTestDB(t)
	seedExportLogs(t, db)

	cases := []struct {
		name    string
		filters url.Values
		wantIDs []string
	}{
		// 导出顺序与 /api/logs 一致（id desc），lane-a 的两条是 id 3、1。
		{name: "lane", filters: url.Values{"lane": {"lane-a"}}, wantIDs: []string{"3", "1"}},
		{name: "channel", filters: url.Values{"channel": {"ch-b"}}, wantIDs: []string{"2"}},
		{name: "key", filters: url.Values{"key": {"key-b"}}, wantIDs: []string{"2"}},
		{name: "model", filters: url.Values{"model": {"model-c"}}, wantIDs: []string{"3"}},
		{name: "success", filters: url.Values{"success": {"false"}}, wantIDs: []string{"2"}},
		{
			name:    "error_kind",
			filters: url.Values{"error_kind": {"soft_rate_limit"}},
			wantIDs: []string{"2"},
		},
		{
			name:    "error_kind no match",
			filters: url.Values{"error_kind": {"hard_auth"}},
			wantIDs: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := runExportLogs(t, exportTestQuery(tc.filters))
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			records, err := csv.NewReader(strings.NewReader(recorder.Body.String())).ReadAll()
			require.NoError(t, err)
			require.NotEmpty(t, records)
			requestIDIndex := 0
			for i, column := range records[0] {
				if column == "request_id" {
					requestIDIndex = i
				}
			}
			var got []string
			for _, row := range records[1:] {
				got = append(got, row[requestIDIndex])
			}
			assert.Equal(t, tc.wantIDs, got)
		})
	}
}

func TestExportLogsWritesAudit(t *testing.T) {
	db := setupLogsExportTestDB(t)
	seedExportLogs(t, db)

	recorder := runExportLogs(t, exportTestQuery(url.Values{"lane": {"lane-a"}}))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var audits []model.PBRAuditLog
	require.NoError(t, db.Where("action = ?", "export").Find(&audits).Error)
	require.Len(t, audits, 1, "一次导出写一条审计")
	audit := audits[0]
	assert.Equal(t, "logs", audit.Resource)
	assert.False(t, audit.DryRun)
	assert.Contains(t, audit.Name, "rows=2")
	assert.Contains(t, audit.Name, "lane=lane-a")
	assert.Len(t, audit.AfterDigest, 64, "过滤摘要以 digest 落库")
}

// 导出与 /api/logs 的 success 口径必须一致：1/true 与 0/false 两种写法都认。
func TestExportLogsSuccessParsingMatchesListLogs(t *testing.T) {
	db := setupLogsExportTestDB(t)
	seedExportLogs(t, db)

	for _, raw := range []string{"1", "true", "TRUE"} {
		recorder := runExportLogs(t, exportTestQuery(url.Values{"success": {raw}}))
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		records, err := csv.NewReader(strings.NewReader(recorder.Body.String())).ReadAll()
		require.NoError(t, err)
		assert.Len(t, records, 3, "success=%s 应命中两条成功行", raw)
	}
	for _, raw := range []string{"0", "false"} {
		recorder := runExportLogs(t, exportTestQuery(url.Values{"success": {raw}}))
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		records, err := csv.NewReader(strings.NewReader(recorder.Body.String())).ReadAll()
		require.NoError(t, err)
		assert.Len(t, records, 2, "success=%s 应只命中失败行", raw)
	}
}
