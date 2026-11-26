package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zzyyyds88/PowerBarRations/model"
)

// 健康端点必须把聚合 outbox 的死信与最近写库失败暴露出来；dead > 0 时整体 degraded。
func TestHealthReportsStatsOutboxAndDBWriteError(t *testing.T) {
	setupAPITestDB(t)
	require.NoError(t, model.DB.Create(&model.PBRStatsOutbox{
		Payload: "not-json", CreatedAt: 1, Attempts: 10, LastError: "decode payload",
	}).Error)
	model.RecordDBWriteError("detail_transaction", assert.AnError)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	Health(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body struct {
		Status           string `json:"status"`
		DB               string `json:"db"`
		DBLastWriteError *struct {
			Stage string `json:"stage"`
		} `json:"db_last_write_error"`
		StatsOutbox struct {
			Pending   int64 `json:"pending"`
			Dead      int64 `json:"dead"`
			Available bool  `json:"available"`
		} `json:"stats_outbox"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, "degraded", body.Status, "死信/近期写失败必须让整体状态降级")
	assert.Equal(t, "degraded", body.DB, "连通但近期写失败按 degraded 报")
	assert.Equal(t, int64(1), body.StatsOutbox.Dead)
	assert.True(t, body.StatsOutbox.Available)
	require.NotNil(t, body.DBLastWriteError)
	assert.Equal(t, "detail_transaction", body.DBLastWriteError.Stage)
}
