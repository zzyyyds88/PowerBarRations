package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /api/v1/routes/{model} 必须对有成员级显式改名的成员回传 upstream_override
// （api-spec §4.4）。此前条件 `UpstreamOverride != UpstreamModel` 恒假——显式名
// 非空且 ≠ 路由键时，解析后的 upstream_model 就等于它，字段从不出现；控制台
// 车道编辑器读不到原值，重新保存时会把全部成员的显式上游真名清空
// （hermes-lane 车道 2026-09-23 实际踩到的数据丢失路径）。

type routeMembersResponse struct {
	Source  string `json:"source"`
	Members []struct {
		Channel          string `json:"channel"`
		UpstreamModel    string `json:"upstream_model"`
		UpstreamOverride string `json:"upstream_override"`
	} `json:"members"`
}

// A. 成员带显式上游真名（≠ 路由键）时，GET /routes 回传 upstream_override。
func TestGetRouteReturnsUpstreamOverride(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "hermes-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "declared-model"}
	require.NoError(t, db.Create(ch).Error)

	require.NoError(t, model.UpsertLane(&model.Lane{Name: "my-lane", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, UpstreamModel: "declared-model", Priority: 1}}}))

	recorder := callAPI(t, http.MethodGet, "/api/v1/routes/my-lane", "", GetRoute,
		gin.Params{{Key: "model", Value: "/my-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp routeMembersResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, model.RouteSourceExplicit, resp.Source)
	require.Len(t, resp.Members, 1)
	assert.Equal(t, "declared-model", resp.Members[0].UpstreamModel)
	assert.Equal(t, "declared-model", resp.Members[0].UpstreamOverride,
		"显式上游真名必须回传 upstream_override，控制台编辑器靠它往返")
}

// B. 模拟控制台编辑器的往返：GET /routes 读 upstream_override → PUT members
// 原样带回 → 显式上游真名不被清空（此前被清空后整条车道必 503/404）。
func TestGetRouteUpstreamOverrideRoundTripsThroughEditorSave(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "hermes-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "declared-model"}
	require.NoError(t, db.Create(ch).Error)

	require.NoError(t, model.UpsertLane(&model.Lane{Name: "my-lane", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, UpstreamModel: "declared-model", Priority: 1}}}))

	// 编辑器读：解析 upstream_override。
	recorder := callAPI(t, http.MethodGet, "/api/v1/routes/my-lane", "", GetRoute,
		gin.Params{{Key: "model", Value: "/my-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var routeResp routeMembersResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &routeResp))
	require.Len(t, routeResp.Members, 1)
	override := routeResp.Members[0].UpstreamOverride
	require.NotEmpty(t, override, "编辑器必须能读到成员的显式上游真名")

	// 编辑器保存：把读到的原值原样发回（lane-composer 的保存载荷形态）。
	body, err := json.Marshal(map[string]any{
		"members": []map[string]any{
			{"channel": "hermes-ch", "upstream_model": override, "priority": 1},
		},
	})
	require.NoError(t, err)
	recorder = callAPI(t, http.MethodPut, "/api/v1/lanes/my-lane/members", string(body), PutLaneMembers,
		gin.Params{{Key: "name", Value: "my-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	// 回读：显式上游真名保留（不是被清成空）。
	recorder = callAPI(t, http.MethodGet, "/api/v1/routes/my-lane", "", GetRoute,
		gin.Params{{Key: "model", Value: "/my-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var after routeMembersResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &after))
	require.Len(t, after.Members, 1)
	assert.Equal(t, "declared-model", after.Members[0].UpstreamModel,
		"编辑器往返后显式上游真名必须保留")
	assert.Equal(t, "declared-model", after.Members[0].UpstreamOverride)
}

// C. 成员没有显式改名（upstream_model 为空，用渠道映射/路由键）时，
// 不回传 upstream_override（空值不出现，保持契约）。
func TestGetRouteOmitsEmptyUpstreamOverride(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "plain-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "plain-model"}
	require.NoError(t, db.Create(ch).Error)

	require.NoError(t, model.UpsertLane(&model.Lane{Name: "plain-lane", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, UpstreamModel: "", Priority: 1}}}))

	recorder := callAPI(t, http.MethodGet, "/api/v1/routes/plain-lane", "", GetRoute,
		gin.Params{{Key: "model", Value: "/plain-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp routeMembersResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Len(t, resp.Members, 1)
	assert.Empty(t, resp.Members[0].UpstreamOverride, "无显式改名的成员不得回传空 upstream_override")
}
