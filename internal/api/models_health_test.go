package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ListModels 对 explicit 车道附带运行态字段（healthy/health/degraded），
// 让路由页的"可调用"反映真实健康而不是只看"车道存在"（routing-spec §7）。
func TestListModelsIncludesLaneHealth(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "health-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "health-model"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "health-model", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Priority: 1}}}))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	ListModels(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, `"healthy_member_count":1`, body)
	assert.Contains(t, body, `"degraded":false`, body)
}
