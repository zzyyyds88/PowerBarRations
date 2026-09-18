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

func callAPI(t *testing.T, method, target, body string, handler gin.HandlerFunc, params gin.Params) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = params
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	c.Request = httptest.NewRequest(method, target, reader)
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c)
	return recorder
}

// GET /api/lane-summaries 一次返回全部车道的成员顺序（不受 cursor 上限影响）。
func TestListLaneSummariesReturnsAllLanesInOrder(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "sum-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "m"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, ch.AddAbilities(nil))
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "sum-lane", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Priority: 5, UpstreamModel: "real"}}}))

	recorder := callAPI(t, http.MethodGet, "/api/v1/lane-summaries", "", ListLaneSummaries, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Items []struct {
			Name    string `json:"name"`
			Members []struct {
				Channel       string `json:"channel"`
				UpstreamModel string `json:"upstream_model"`
				Priority      int    `json:"priority"`
				ChannelEnable bool   `json:"channel_enabled"`
			} `json:"members"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	assert.Equal(t, "sum-lane", resp.Items[0].Name)
	require.Len(t, resp.Items[0].Members, 1)
	assert.Equal(t, "sum-ch", resp.Items[0].Members[0].Channel)
	assert.Equal(t, "real", resp.Items[0].Members[0].UpstreamModel)
	assert.True(t, resp.Items[0].Members[0].ChannelEnable)
}

// 模型元数据 upsert → 读回；仅由渠道声明的模型以 has_metadata=false 出现。
func TestModelMetadataUpsertAndList(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "meta-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "declared-only"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, ch.AddAbilities(nil))

	// 写入目录记录
	body := `{"description":"a pooled model","icon":"openai","tags":["chat","fast"],"status":1}`
	recorder := callAPI(t, http.MethodPut, "/api/v1/model-metadata/catalog-model", body, PutModelMetadata,
		gin.Params{{Key: "model", Value: "catalog-model"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "a pooled model")

	// 列表同时包含目录记录与仅渠道声明的模型
	recorder = callAPI(t, http.MethodGet, "/api/v1/model-metadata", "", ListModelMetadata, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Items []struct {
			Model        string `json:"model"`
			HasMetadata  bool   `json:"has_metadata"`
			Description  string `json:"description"`
			ChannelCount int    `json:"configured_channel_count"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	byModel := map[string]bool{}
	for _, item := range resp.Items {
		byModel[item.Model] = item.HasMetadata
		if item.Model == "declared-only" {
			assert.Equal(t, 1, item.ChannelCount)
		}
	}
	assert.True(t, byModel["catalog-model"])
	declared, ok := byModel["declared-only"]
	require.True(t, ok, "仅由渠道声明的模型也必须出现")
	assert.False(t, declared)
}

// 删除目录记录：remove_from_channels 且被车道引用时 409。
func TestDeleteModelMetadataBlockedByLaneReference(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "del-meta-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "meta-lane-model"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, ch.AddAbilities(nil))
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "meta-lane-model", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Priority: 1}}}))
	require.NoError(t, (&model.Model{ModelName: "meta-lane-model"}).Insert())

	recorder := callAPI(t, http.MethodDelete,
		"/api/v1/model-metadata/meta-lane-model?remove_from_channels=true", "",
		DeleteModelMetadataByModel, gin.Params{{Key: "model", Value: "meta-lane-model"}})
	assert.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "conflict")
}
