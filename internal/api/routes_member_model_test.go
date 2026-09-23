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

// GET /api/v1/routes/{model} 的成员模型语义（ADR 0008）：
// 成员只存**所选模型** `model`，`upstream_model` 是由渠道 `model_mapping` 推导的
// **只读派生真名**，成员级覆盖已整体移除（不再有 `upstream_override`）。
//
// 本文件取代原先的 routes_upstream_override_test.go——那三个用例测的正是被移除的
// 覆盖能力（含"显式真名经 upstream_override 往返"），新口径下不成立。

type routeMembersResponse struct {
	Source  string `json:"source"`
	Members []struct {
		Channel       string `json:"channel"`
		Model         string `json:"model"`
		UpstreamModel string `json:"upstream_model"`
		PublicAlias   string `json:"public_alias"`
	} `json:"members"`
}

// A. 无映射时真名 = 所选模型；成员回传 `model` 且 `upstream_model` 与之一致。
func TestGetRouteReturnsModelAndDerivedUpstream(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "plain-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "declared-model"}
	require.NoError(t, db.Create(ch).Error)

	require.NoError(t, model.UpsertLane(&model.Lane{Name: "plain-lane", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Model: "declared-model", Priority: 1}}}))

	recorder := callAPI(t, http.MethodGet, "/api/v1/routes/plain-lane", "", GetRoute,
		gin.Params{{Key: "model", Value: "/plain-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp routeMembersResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, model.RouteSourceExplicit, resp.Source)
	require.Len(t, resp.Members, 1)
	assert.Equal(t, "declared-model", resp.Members[0].Model, "model = 成员所选模型（写回用它）")
	assert.Equal(t, "declared-model", resp.Members[0].UpstreamModel,
		"无渠道映射时派生真名就是所选模型本身")
}

// B. 有渠道映射时真名 = 映射右值，且**改映射立即生效、不必重存车道**。
// 这是 ADR 0008 要修复的核心缺陷：此前加入成员时把映射物化写入成员字段，
// 后续改映射对该成员完全失效。
func TestRouteUpstreamFollowsChannelMappingWithoutResavingLane(t *testing.T) {
	db := setupAPITestDB(t)
	mapping := `{"declared-model":"vendor/v1"}`
	ch := &model.Channel{Name: "map-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "declared-model", ModelMapping: &mapping}
	require.NoError(t, db.Create(ch).Error)

	require.NoError(t, model.UpsertLane(&model.Lane{Name: "map-lane", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Model: "declared-model", Priority: 1}}}))

	read := func() routeMembersResponse {
		t.Helper()
		recorder := callAPI(t, http.MethodGet, "/api/v1/routes/map-lane", "", GetRoute,
			gin.Params{{Key: "model", Value: "/map-lane"}})
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		var resp routeMembersResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
		return resp
	}

	first := read()
	require.Len(t, first.Members, 1)
	assert.Equal(t, "declared-model", first.Members[0].Model)
	assert.Equal(t, "vendor/v1", first.Members[0].UpstreamModel)

	// 只改渠道映射（**不动车道**），真名必须立即跟随。
	updated := `{"declared-model":"vendor/v2"}`
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", ch.Id).
		Update("model_mapping", updated).Error)
	model.InitChannelCache()

	second := read()
	require.Len(t, second.Members, 1)
	assert.Equal(t, "declared-model", second.Members[0].Model, "成员存的仍是所选模型，未被真名污染")
	assert.Equal(t, "vendor/v2", second.Members[0].UpstreamModel,
		"改渠道映射后真名立即生效，无需重存车道（ADR 0008）")
}

// C. 池化车道：查表键是**成员所选模型**，与车道路由键无关。
// 此前运行期用路由键查表，池化场景必然查错（ADR 0008 背景第 3 条）。
func TestRouteUpstreamLookupKeyIsMemberModelNotRouteKey(t *testing.T) {
	db := setupAPITestDB(t)
	// 映射里同时给出"按模型名"与"按路由键"两条，键不同、右值不同：
	// 只有用成员所选模型查表才能得到 up/from-model。
	mapping := `{"member-model":"up/from-model","pool-lane":"up/from-route-key"}`
	ch := &model.Channel{Name: "pool-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "member-model", ModelMapping: &mapping}
	require.NoError(t, db.Create(ch).Error)

	// 车道路由键（pool-lane）≠ 成员所选模型（member-model）。
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "pool-lane", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Model: "member-model", Priority: 1}}}))

	recorder := callAPI(t, http.MethodGet, "/api/v1/routes/pool-lane", "", GetRoute,
		gin.Params{{Key: "model", Value: "/pool-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp routeMembersResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Len(t, resp.Members, 1)
	assert.Equal(t, "up/from-model", resp.Members[0].UpstreamModel,
		"查表键必须是成员所选模型 member-model，不是车道路由键 pool-lane")
}

// D. 编辑器往返：读 `model` → 原样带回 → 成员不丢、真名不变。
// 成员级真名不再是可写字段，所以这里断言的是"模型名往返"，而非旧的 upstream_override。
func TestRouteMemberModelRoundTripsThroughEditorSave(t *testing.T) {
	db := setupAPITestDB(t)
	mapping := `{"declared-model":"vendor/real"}`
	ch := &model.Channel{Name: "rt-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "declared-model", ModelMapping: &mapping}
	require.NoError(t, db.Create(ch).Error)

	require.NoError(t, model.UpsertLane(&model.Lane{Name: "rt-lane", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Model: "declared-model", Priority: 1}}}))

	recorder := callAPI(t, http.MethodGet, "/api/v1/routes/rt-lane", "", GetRoute,
		gin.Params{{Key: "model", Value: "/rt-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var before routeMembersResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &before))
	require.Len(t, before.Members, 1)
	require.Equal(t, "declared-model", before.Members[0].Model)

	// 编辑器保存：只回传 model（成员级真名不是可写字段）。
	body, err := json.Marshal(map[string]any{
		"members": []map[string]any{
			{"channel": "rt-ch", "model": before.Members[0].Model, "priority": 1},
		},
	})
	require.NoError(t, err)
	recorder = callAPI(t, http.MethodPut, "/api/v1/lanes/rt-lane/members", string(body), PutLaneMembers,
		gin.Params{{Key: "name", Value: "rt-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	recorder = callAPI(t, http.MethodGet, "/api/v1/routes/rt-lane", "", GetRoute,
		gin.Params{{Key: "model", Value: "/rt-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var after routeMembersResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &after))
	require.Len(t, after.Members, 1)
	assert.Equal(t, "declared-model", after.Members[0].Model, "模型名往返不丢")
	assert.Equal(t, "vendor/real", after.Members[0].UpstreamModel, "真名仍由映射推导")
}

// E. 成员唯一键 = (渠道, 所选模型)：同一对重复提交 422 duplicate_member；
// 同一渠道的不同模型仍可共存（ADR 0008）。
func TestPutLaneMembersRejectsDuplicateChannelModelPair(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "dup-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "m-1,m-2"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "dup-lane", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Model: "m-1", Priority: 1}}}))

	// 同一 (渠道, 模型) 两次 → 422。
	body := `{"members":[
		{"channel":"dup-ch","model":"m-1","priority":2},
		{"channel":"dup-ch","model":"m-1","priority":1}
	]}`
	recorder := callAPI(t, http.MethodPut, "/api/v1/lanes/dup-lane/members", body, PutLaneMembers,
		gin.Params{{Key: "name", Value: "dup-lane"}})
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "duplicate_member")

	// 同一渠道的不同模型 → 允许，两个成员都在。
	body = `{"members":[
		{"channel":"dup-ch","model":"m-1","priority":2},
		{"channel":"dup-ch","model":"m-2","priority":1}
	]}`
	recorder = callAPI(t, http.MethodPut, "/api/v1/lanes/dup-lane/members", body, PutLaneMembers,
		gin.Params{{Key: "name", Value: "dup-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	recorder = callAPI(t, http.MethodGet, "/api/v1/routes/dup-lane", "", GetRoute,
		gin.Params{{Key: "model", Value: "/dup-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp routeMembersResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Len(t, resp.Members, 2, "同一渠道的不同模型必须是两个成员")
	assert.Equal(t, "m-1", resp.Members[0].Model)
	assert.Equal(t, "m-2", resp.Members[1].Model)
}

// F. `model` 是必填：缺了它就没有成员身份，必须 400 而不是静默落一个空成员。
func TestPutLaneMembersRequiresModel(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "req-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "m-1"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "req-lane", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Model: "m-1", Priority: 1}}}))

	body := `{"members":[{"channel":"req-ch","priority":1}]}`
	recorder := callAPI(t, http.MethodPut, "/api/v1/lanes/req-lane/members", body, PutLaneMembers,
		gin.Params{{Key: "name", Value: "req-lane"}})
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "validation_failed")
}
