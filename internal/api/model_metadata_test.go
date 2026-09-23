package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
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
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "sum-lane", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Priority: 5, Model: "real"}}}))

	recorder := callAPI(t, http.MethodGet, "/api/v1/lane-summaries", "", ListLaneSummaries, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Items []struct {
			Name    string `json:"name"`
			Members []struct {
				Channel       string `json:"channel"`
				UpstreamModel string `json:"model"`
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

// 规则条目（前缀/包含/后缀）可写入并写后回读；越界一律 422 validation_failed。
func TestModelMetadataPutAcceptsNameRules(t *testing.T) {
	setupAPITestDB(t)
	cases := []struct {
		name string
		rule int
	}{
		{name: "prefix", rule: model.NameRulePrefix},
		{name: "contains", rule: model.NameRuleContains},
		{name: "suffix", rule: model.NameRuleSuffix},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"description":"rule %s","name_rule":%d,"status":1}`, tc.name, tc.rule)
			recorder := callAPI(t, http.MethodPut, "/api/v1/model-metadata/qwen3-", body, PutModelMetadata,
				gin.Params{{Key: "model", Value: "qwen3-"}})
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			var saved struct {
				Model    string `json:"model"`
				NameRule int    `json:"name_rule"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &saved))
			assert.Equal(t, "qwen3-", saved.Model)
			assert.Equal(t, tc.rule, saved.NameRule, "PUT 响应必须写后回读 name_rule")

			// 列表面同样回读该规则值
			listRecorder := callAPI(t, http.MethodGet, "/api/v1/model-metadata", "", ListModelMetadata, nil)
			require.Equal(t, http.StatusOK, listRecorder.Code)
			var list struct {
				Items []struct {
					Model    string `json:"model"`
					NameRule int    `json:"name_rule"`
				} `json:"items"`
			}
			require.NoError(t, json.Unmarshal(listRecorder.Body.Bytes(), &list))
			found := false
			for _, item := range list.Items {
				if item.Model == "qwen3-" {
					found = true
					assert.Equal(t, tc.rule, item.NameRule)
				}
			}
			assert.True(t, found, "规则条目必须出现在稳定面列表里")
		})
	}
}

// name_rule 越界（4 / -1）与非法 status 返回 422 validation_failed，且不落库。
func TestModelMetadataPutRejectsOutOfRangeNameRule(t *testing.T) {
	db := setupAPITestDB(t)
	for _, rule := range []string{"4", "-1"} {
		recorder := callAPI(t, http.MethodPut, "/api/v1/model-metadata/oob-model",
			fmt.Sprintf(`{"name_rule":%s,"status":1}`, rule), PutModelMetadata,
			gin.Params{{Key: "model", Value: "oob-model"}})
		assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code, "name_rule=%s 应 422", rule)
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
		assert.Equal(t, apierr.CodeValidationFailed, envelope.Error.Code)
		assert.Contains(t, envelope.Error.Message, "matching rule")
	}
	var count int64
	require.NoError(t, db.Model(&model.Model{}).Where("model_name = ?", "oob-model").Count(&count).Error)
	assert.Zero(t, count, "校验失败的写入不得落库")
}

// 规则条目的 configured_channel_count 按 MatchesName 命中集统计去重渠道数；
// 精确条目仍按精确名查表；稳定面不返回 matched_count / matched_models。
func TestModelMetadataListCountsChannelsByMatchedNames(t *testing.T) {
	db := setupAPITestDB(t)
	// 三个渠道声明不同的 qwen3-* 模型名；ch-sibling 另声明一个不相关模型。
	channels := []*model.Channel{
		{Name: "ch-a", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "qwen3-max,qwen3-mini"},
		{Name: "ch-b", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "qwen3-flash"},
		{Name: "ch-c", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "qwen3-mini,gpt-4o"},
	}
	for _, ch := range channels {
		require.NoError(t, db.Create(ch).Error)
	}
	require.NoError(t, (&model.Model{ModelName: "qwen3-", NameRule: model.NameRulePrefix, Status: 1}).Insert())
	require.NoError(t, (&model.Model{ModelName: "gpt-4o", NameRule: model.NameRuleExact, Status: 1}).Insert())

	recorder := callAPI(t, http.MethodGet, "/api/v1/model-metadata", "", ListModelMetadata, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	var raw struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &raw))
	byModel := map[string]map[string]any{}
	for _, item := range raw.Items {
		byModel[item["model"].(string)] = item
	}

	prefixItem, ok := byModel["qwen3-"]
	require.True(t, ok, "前缀规则条目必须出现")
	assert.Equal(t, float64(model.NameRulePrefix), prefixItem["name_rule"])
	// qwen3-max/qwen3-mini/qwen3-flash 三条声明分别落在 ch-a、ch-a、ch-b、ch-c → 去重 3 个渠道
	assert.Equal(t, float64(3), prefixItem["configured_channel_count"],
		"前缀规则应按命中模型名集合统计去重渠道数，而不是恒为 0")

	exactItem, ok := byModel["gpt-4o"]
	require.True(t, ok)
	assert.Equal(t, float64(1), exactItem["configured_channel_count"],
		"精确条目仍只统计声明了同名模型的渠道")

	for name, item := range byModel {
		_, hasMatchedCount := item["matched_count"]
		_, hasMatchedModels := item["matched_models"]
		assert.False(t, hasMatchedCount, "%s 的稳定面响应不得含 matched_count", name)
		assert.False(t, hasMatchedModels, "%s 的稳定面响应不得含 matched_models", name)
	}
}

// PUT 写后回读的 configured_channel_count 必须与 GET 列表同口径：规则条目此前恒回 0，
// AI/脚本会据此误判"这条规则没命中任何渠道"。
func TestModelMetadataPutEchoesMatchedChannelCount(t *testing.T) {
	db := setupAPITestDB(t)
	channels := []*model.Channel{
		{Name: "echo-a", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "qwen3-max,qwen3-mini"},
		{Name: "echo-b", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-4o"},
	}
	for _, ch := range channels {
		require.NoError(t, db.Create(ch).Error)
	}

	recorder := callAPI(t, http.MethodPut, "/api/v1/model-metadata/qwen3-",
		`{"description":"qwen3 系列","status":1,"name_rule":1}`, PutModelMetadata,
		gin.Params{{Key: "model", Value: "qwen3-"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var saved struct {
		Model                  string `json:"model"`
		NameRule               int    `json:"name_rule"`
		ConfiguredChannelCount int    `json:"configured_channel_count"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &saved))
	assert.Equal(t, "qwen3-", saved.Model)
	assert.Equal(t, model.NameRulePrefix, saved.NameRule)
	assert.Equal(t, 1, saved.ConfiguredChannelCount,
		"写后回读须按命中模型名集合统计去重渠道数（只有 echo-a 声明了 qwen3-*）")

	var raw map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &raw))
	_, hasMatchedCount := raw["matched_count"]
	_, hasMatchedModels := raw["matched_models"]
	assert.False(t, hasMatchedCount, "稳定面写后回读同样不得含 matched_count")
	assert.False(t, hasMatchedModels, "稳定面写后回读同样不得含 matched_models")
}

// 规则条目可删目录记录本身；?remove_from_channels=true 仍只对精确名允许。
func TestDeleteModelMetadataRuleEntry(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "rule-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "qwen3-max"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, (&model.Model{ModelName: "qwen3-", NameRule: model.NameRulePrefix, Status: 1}).Insert())

	recorder := callAPI(t, http.MethodDelete, "/api/v1/model-metadata/qwen3-", "",
		DeleteModelMetadataByModel, gin.Params{{Key: "model", Value: "qwen3-"}})
	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var remaining int64
	require.NoError(t, db.Model(&model.Model{}).Where("model_name = ?", "qwen3-").Count(&remaining).Error)
	assert.Zero(t, remaining, "规则记录本身应被删除")
	// 命中集里的真实模型不受影响：渠道声明仍在。
	declaring, err := model.GetChannelsDeclaringModel("qwen3-max")
	require.NoError(t, err)
	assert.NotEmpty(t, declaring, "删规则不应摘除被命中模型的渠道声明")
}

// 规则条目带 remove_from_channels 时必须被拒绝且不得删记录（model/model_meta.go:238 守卫）。
//
// 注意：api-spec §5.7 要求该情形返回 422，当前稳定面把 model 层的普通 error 经
// writeAPIError 映射成 500 internal_error。要改成 422 必须动 model/ 或 controller/
// （本 workstream 边界禁止），故此处先钉住"拒绝 + 不删数据"这一实质不变量，
// 状态码差异已在交付报告中列为待主线决策项。
func TestDeleteModelMetadataRuleRejectsRemoveFromChannels(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "rule-del-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "qwen3-max"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, (&model.Model{ModelName: "qwen3-suffix", NameRule: model.NameRuleSuffix, Status: 1}).Insert())

	recorder := callAPI(t, http.MethodDelete,
		"/api/v1/model-metadata/qwen3-suffix?remove_from_channels=true", "",
		DeleteModelMetadataByModel, gin.Params{{Key: "model", Value: "qwen3-suffix"}})
	// api-spec §3：参数级拒绝不得冒成 500；规则条目带 remove_from_channels 属 422。
	assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), apierr.CodeValidationFailed)
	assert.Contains(t, recorder.Body.String(), "exact-match")
	var remaining int64
	require.NoError(t, db.Model(&model.Model{}).Where("model_name = ?", "qwen3-suffix").Count(&remaining).Error)
	assert.Equal(t, int64(1), remaining, "被拒绝的删除不得移除规则记录")
	declaring, err := model.GetChannelsDeclaringModel("qwen3-max")
	require.NoError(t, err)
	assert.NotEmpty(t, declaring, "被拒绝的删除不得摘除渠道声明")
}

// 删除目录记录：remove_from_channels 且被车道引用时 409。
func TestDeleteModelMetadataBlockedByLaneReference(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "del-meta-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "meta-lane-model"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "meta-lane-model", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Priority: 1}}}))
	require.NoError(t, (&model.Model{ModelName: "meta-lane-model"}).Insert())

	recorder := callAPI(t, http.MethodDelete,
		"/api/v1/model-metadata/meta-lane-model?remove_from_channels=true", "",
		DeleteModelMetadataByModel, gin.Params{{Key: "model", Value: "meta-lane-model"}})
	assert.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "conflict")
}
