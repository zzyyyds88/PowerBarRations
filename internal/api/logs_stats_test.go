package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"pbr/common"
	"pbr/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupStatsAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stats-api-test.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PBRStatsHourly{}))
	previous := model.DB
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() { model.DB = previous })
	return db
}

func runGetStats(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/stats?"+query, nil)
	GetStats(c)
	return recorder
}

// GET /api/v1/stats 的 group_by 白名单必须接受新增的 channel_model 维度（breakdown W9）。
func TestGetStatsAcceptsChannelModelGroupBy(t *testing.T) {
	db := setupStatsAPITestDB(t)
	baseTs := int64(1_700_000_000)
	require.NoError(t, db.Create(&model.PBRStatsHourly{
		BucketTs:         baseTs,
		GroupKind:        "channel_model",
		GroupKey:         "ch-a" + model.PBRChannelModelSeparator + "m-a",
		Requests:         2,
		Successes:        1,
		PromptTokens:     10,
		CompletionTokens: 5,
		CostSum:          0.25,
	}).Error)

	recorder := runGetStats(t, "granularity=hour&group_by=channel_model")
	assert.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, "\"group_by\":\"channel_model\"")
	assert.Contains(t, body, "\"group\":\"ch-a"+model.PBRChannelModelSeparator+"m-a\"")
	assert.Contains(t, body, "\"requests\":2")
}

// 非法 group_by 必须报错，不得静默回落到 lane（否则调用方拿到错误维度的数据）。
func TestGetStatsRejectsUnknownGroupBy(t *testing.T) {
	setupStatsAPITestDB(t)
	recorder := runGetStats(t, "group_by=not_a_dimension")
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "validation_failed")
}
