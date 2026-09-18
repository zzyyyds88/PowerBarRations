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

// 历史悬空成员（渠道已删除）此前没有任何界面/接口可清理；
// POST /lanes/cleanup-members 移除它们，成员清空的车道整条删除。
func TestCleanupLaneMembersRemovesOrphans(t *testing.T) {
	db := setupAPITestDB(t)
	keep := &model.Channel{Name: "keep-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "mix-model"}
	require.NoError(t, db.Create(keep).Error)
	require.NoError(t, keep.AddAbilities(nil))
	// 车道 A：一个有效成员 + 一个指向不存在渠道的悬空成员 → 清理后保留。
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "mix-model", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: keep.Id, Priority: 2}, {ChannelId: 999999, Priority: 1}}}))
	// 车道 B：只有悬空成员 → 整条删除。
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "orphan-only", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: 888888, Priority: 1}}}))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/lanes/cleanup-members", nil)
	CleanupLaneMembers(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := recorder.Body.String()
	assert.Contains(t, body, "mix-model")
	assert.Contains(t, body, "orphan-only")

	mixed, err := model.GetLaneByName("mix-model")
	require.NoError(t, err)
	require.Len(t, mixed.Members, 1, "悬空成员应被移除")
	assert.Equal(t, keep.Id, mixed.Members[0].ChannelId)
	_, err = model.GetLaneByName("orphan-only")
	assert.Error(t, err, "只剩悬空成员的车道应被删除")
}
