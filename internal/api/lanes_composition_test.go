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

// ADR 0006：车道成员自由编排。成员唯一键是 (channel, upstream_model)，
// 与路由键解耦；同一渠道可在一条车道内出现多次，成员渠道也无需声明该路由键。

// laneMembersResponse 是 GET /api/v1/lanes/{name} 响应里成员的最小可断言形态。
type laneMembersResponse struct {
	Name    string `json:"name"`
	Members []struct {
		Channel       string `json:"channel"`
		UpstreamModel string `json:"upstream_model"`
		Priority      int    `json:"priority"`
	} `json:"members"`
}

// A. 同一渠道两个不同 upstream_model 可作为两个成员写入，回读得到两个成员，
// 且按 priority 降序（数字大者优先）。
func TestPutLaneAllowsTwoModelsFromSameChannel(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "pool-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "a-model-1,a-model-2"}
	require.NoError(t, db.Create(ch).Error)

	body := `{"members":[
		{"channel":"pool-ch","upstream_model":"a-model-1","priority":10},
		{"channel":"pool-ch","upstream_model":"a-model-2","priority":20}
	]}`
	recorder := callAPI(t, http.MethodPut, "/api/v1/lanes/fast", body, PutLane,
		gin.Params{{Key: "name", Value: "fast"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var putResp laneMembersResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &putResp))
	require.Len(t, putResp.Members, 2, "同一渠道的两个不同上游模型必须是两个成员（不得按渠道去重）")

	// 回读 GET：顺序按 priority 降序（20 在前，10 在后）。
	recorder = callAPI(t, http.MethodGet, "/api/v1/lanes/fast", "", GetLane,
		gin.Params{{Key: "name", Value: "fast"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var getResp laneMembersResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &getResp))
	require.Len(t, getResp.Members, 2, "回读必须与写入一致（两个成员）")
	assert.Equal(t, "pool-ch", getResp.Members[0].Channel)
	assert.Equal(t, "a-model-2", getResp.Members[0].UpstreamModel, "priority 大者排前")
	assert.Equal(t, 20, getResp.Members[0].Priority)
	assert.Equal(t, "a-model-1", getResp.Members[1].UpstreamModel)
	assert.Equal(t, 10, getResp.Members[1].Priority)

	// 库里也应确实有两条成员记录（不是响应层拼出来的）。
	saved, err := model.GetLaneByName("fast")
	require.NoError(t, err)
	require.Len(t, saved.Members, 2)
	assert.Equal(t, ch.Id, saved.Members[0].ChannelId)
	assert.Equal(t, ch.Id, saved.Members[1].ChannelId)
}

// B. 成员渠道没有声明该路由键（池化）时，PUT 仍 200 且回读一致。
func TestPutLaneAllowsMemberChannelWithoutRouteKey(t *testing.T) {
	db := setupAPITestDB(t)
	// 渠道只声明了 declared-model，完全不认识路由键 pooled-lane。
	ch := &model.Channel{Name: "no-key-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "declared-model"}
	require.NoError(t, db.Create(ch).Error)

	body := `{"members":[{"channel":"no-key-ch","upstream_model":"declared-model","priority":5}]}`
	recorder := callAPI(t, http.MethodPut, "/api/v1/lanes/pooled-lane", body, PutLane,
		gin.Params{{Key: "name", Value: "pooled-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, "ADR 0006：PUT 不校验成员是否声明该路由键：%s", recorder.Body.String())

	recorder = callAPI(t, http.MethodGet, "/api/v1/lanes/pooled-lane", "", GetLane,
		gin.Params{{Key: "name", Value: "pooled-lane"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var getResp laneMembersResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &getResp))
	require.Len(t, getResp.Members, 1)
	assert.Equal(t, "no-key-ch", getResp.Members[0].Channel)
	assert.Equal(t, "declared-model", getResp.Members[0].UpstreamModel)

	// 渠道声明里确实没有该路由键（确认测试场景成立）。
	assert.NotContains(t, ch.GetModels(), "pooled-lane")
}

// C. PUT /channels/{name} 新增模型后：新模型 source=unconfigured、routable=false，
// 且车道总数不变（声明模型绝不自动建车道）。
func TestPutChannelNewModelDoesNotCreateLane(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "grow-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "existing-model"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "existing-model", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Priority: 1}}}))

	// 基线：当前只有 existing-model 一条车道。
	recorder := callAPI(t, http.MethodGet, "/api/v1/lanes", "", ListLanes, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	var lanesBefore struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &lanesBefore))
	require.Len(t, lanesBefore.Items, 1)
	assert.Equal(t, "existing-model", lanesBefore.Items[0].Name)

	// 给渠道新增一个模型（声明行为）。
	recorder = callPutChannel(t, "grow-ch", `{"models":["existing-model","brand-new-model"]}`, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	// 新模型在 /models 里是 unconfigured / routable=false。
	recorder = callAPI(t, http.MethodGet, "/api/v1/models", "", ListModels, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	var modelsResp struct {
		Items []struct {
			Model    string `json:"model"`
			Source   string `json:"source"`
			Routable bool   `json:"routable"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &modelsResp))
	var found bool
	for _, item := range modelsResp.Items {
		if item.Model == "brand-new-model" {
			found = true
			assert.Equal(t, model.RouteSourceUnconfigured, item.Source, "新增声明模型不得自动成链")
			assert.False(t, item.Routable, "未建车道的模型不可调用")
		}
	}
	require.True(t, found, "新增声明的模型必须出现在 /models")

	// 车道清单不包含任何新车道，总数不变。
	recorder = callAPI(t, http.MethodGet, "/api/v1/lanes", "", ListLanes, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	var lanesAfter struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &lanesAfter))
	require.Len(t, lanesAfter.Items, len(lanesBefore.Items), "声明模型后车道总数必须不变")
	for _, item := range lanesAfter.Items {
		assert.NotEqual(t, "brand-new-model", item.Name, "声明模型绝不自动建车道")
	}
}
