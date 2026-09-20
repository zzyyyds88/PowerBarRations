package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupLogsAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logs-api-test.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PBRRequestLog{}))
	previous := model.DB
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() { model.DB = previous })
	return db
}

func runListLogs(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/logs?"+query, nil)
	ListLogs(c)
	return recorder
}

// GET /api/v1/logs?page=2&page_size=10 走偏移分页：响应含 items/total/page/page_size，
// 切片是该页的精确 10 条（不含 +1 探测），total 不受分页影响。
func TestListLogsOffsetPagination(t *testing.T) {
	db := setupLogsAPITestDB(t)
	for i := 0; i < 25; i++ {
		require.NoError(t, db.Create(&model.PBRRequestLog{
			Ts:                int64(1_700_000_000 + i),
			LaneName:          "lane-x",
			MemberChannelName: "ch-x",
			TokenName:         "key-x",
			RequestModel:      "model-x",
			Success:           i%2 == 0,
			Type:              model.LogTypeConsume,
			UserId:            7,
			Ip:                "127.0.0.1",
		}).Error)
	}

	rec := runListLogs(t, "page=2&page_size=10")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items      []map[string]any `json:"items"`
		Total      json.Number      `json:"total"`
		Page       json.Number      `json:"page"`
		PageSize   json.Number      `json:"page_size"`
		NextCursor *string          `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body.Items, 10, "page2 应返回 10 条")
	assert.Equal(t, "25", body.Total.String())
	assert.Equal(t, "2", body.Page.String())
	assert.Equal(t, "10", body.PageSize.String())
	assert.Nil(t, body.NextCursor, "偏移模式不应返回 next_cursor")
}

// GET /api/v1/logs（不传 page）走游标分页：响应含 items/next_cursor，向后兼容。
func TestListLogsCursorBackwardCompatible(t *testing.T) {
	db := setupLogsAPITestDB(t)
	for i := 0; i < 15; i++ {
		require.NoError(t, db.Create(&model.PBRRequestLog{
			Ts:       int64(1_700_000_000 + i),
			LaneName: "lane-x",
			Success:  true,
			Type:     model.LogTypeConsume,
		}).Error)
	}

	rec := runListLogs(t, "limit=10")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items      []map[string]any `json:"items"`
		NextCursor *string          `json:"next_cursor"`
		Total      *json.Number     `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body.Items, 10, "游标模式按 limit 返回")
	assert.NotNil(t, body.NextCursor, "还有更多数据，应有 next_cursor")
	assert.Nil(t, body.Total, "游标模式不应返回 total")
}

// logResponse 必须输出 type/channel_id/token_id/user_id/ip（对齐 UsageLog 形状）。
// 此前 logResponse 漏输出这些字段，前端类型声明了却拿不到值。
func TestLogResponseIncludesUsageLogFields(t *testing.T) {
	db := setupLogsAPITestDB(t)
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts:                1_700_000_000,
		LaneName:          "lane-a",
		MemberChannelId:   42,
		MemberChannelName: "ch-a",
		TokenId:           9,
		TokenName:         "key-a",
		UserId:            7,
		Username:          "admin",
		Ip:                "10.0.0.1",
		Type:              model.LogTypeConsume,
		Success:           true,
	}).Error)

	rec := runListLogs(t, "page=1&page_size=10")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	item := body.Items[0]
	assert.Equal(t, float64(42), item["channel_id"], "channel_id 必须输出（前端 UsageLog.channel 用）")
	assert.Equal(t, float64(9), item["token_id"])
	assert.Equal(t, float64(7), item["user_id"])
	assert.Equal(t, "admin", item["username"])
	assert.Equal(t, float64(model.LogTypeConsume), item["type"])
	assert.Equal(t, "10.0.0.1", item["ip"])
}

// page 非正整数必须 400（静默回落第一页会让调用方看不到翻页错误）。
func TestListLogsRejectsBadPage(t *testing.T) {
	setupLogsAPITestDB(t)
	rec := runListLogs(t, "page=0&page_size=10")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "validation_failed")
}
