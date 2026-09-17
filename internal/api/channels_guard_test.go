package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callPutChannel 以最小请求体调用 PUT /api/v1/channels/{name}。
func callPutChannel(t *testing.T, name, body, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "name", Value: name}}
	url := "/api/v1/channels/" + name
	if query != "" {
		url += "?" + query
	}
	c.Request = httptest.NewRequest(http.MethodPut, url, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	PutChannel(c)
	return recorder
}

// 从渠道移除仍被车道引用的模型：默认 409 且不动车道；force=1 覆盖并清理（空车道删除）。
func TestPutChannelBlocksModelRemovalReferencedByLane(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "guard-ch", Type: 1, Key: "sk-guard", Status: common.ChannelStatusEnabled, Group: "default", Models: "keep-me,drop-me"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, ch.AddAbilities(nil))
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "drop-me", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Priority: 1}}}))

	// 默认阻断：409 + 车道清单，不写库
	recorder := callPutChannel(t, "guard-ch", `{"models":["keep-me"]}`, "")
	require.Equal(t, http.StatusConflict, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "refusing to remove models still referenced by lanes")
	_, err := model.GetLaneByName("drop-me")
	require.NoError(t, err, "被阻断时车道必须保留")

	// force=1：继续更新并清理，车道无其它成员 → 整条删除
	recorder = callPutChannel(t, "guard-ch", `{"models":["keep-me"]}`, "force=1")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.Contains(t, resp, "deleted_lanes")
	_, err = model.GetLaneByName("drop-me")
	assert.Error(t, err, "force 清理后空车道应被删除")
}

// force 清理只移除本渠道成员；仍有他渠道成员的车道保留。
func TestPutChannelCleanupKeepsLaneWithOtherMembers(t *testing.T) {
	db := setupAPITestDB(t)
	chA := &model.Channel{Name: "clean-a", Type: 1, Key: "sk-a", Status: common.ChannelStatusEnabled, Group: "default", Models: "m-drop"}
	require.NoError(t, db.Create(chA).Error)
	require.NoError(t, chA.AddAbilities(nil))
	chB := &model.Channel{Name: "clean-b", Type: 1, Key: "sk-b", Status: common.ChannelStatusEnabled, Group: "default", Models: "m-drop"}
	require.NoError(t, db.Create(chB).Error)
	require.NoError(t, chB.AddAbilities(nil))
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "m-drop", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: chA.Id, Priority: 2}, {ChannelId: chB.Id, Priority: 1}}}))

	recorder := callPutChannel(t, "clean-a", `{"models":["keep-only"]}`, "force=1")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	lane, err := model.GetLaneByName("m-drop")
	require.NoError(t, err)
	var members []model.LaneMember
	require.NoError(t, db.Where("lane_id = ?", lane.Id).Find(&members).Error)
	require.Len(t, members, 1, "只应移除被编辑渠道的成员")
	assert.Equal(t, chB.Id, members[0].ChannelId)
}
