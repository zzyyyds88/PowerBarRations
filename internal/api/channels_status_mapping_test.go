package api

// 渠道状态码映射（ADR 0013）：只改客户端可见状态码，必须是 400–599 且禁 503。
// v1 渠道契约、导出/导入与转发层共用同一份语义，这里守住写入口与回读。

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func channelStatusMappingBody(mapping string) string {
	return `{"name":"ch-map","type":"openai","base_url":"https://upstream.example/v1",` +
		`"models":["model-a"],"status_code_mapping":` + mapping + `}`
}

func TestPutChannelStatusCodeMappingRoundTrip(t *testing.T) {
	setupAPITestDB(t)

	recorder := putChannelJSON(t, "ch-map", "", channelStatusMappingBody(`{"429":"400"}`))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var response struct {
		StatusCodeMapping map[string]string `json:"status_code_mapping"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, map[string]string{"429": "400"}, response.StatusCodeMapping)

	var stored model.Channel
	require.NoError(t, model.DB.Where("name = ?", "ch-map").First(&stored).Error)
	assert.JSONEq(t, `{"429":"400"}`, stored.GetStatusCodeMapping())

	// 省略字段保持原值。
	recorder = putChannelJSON(t, "ch-map", "",
		`{"name":"ch-map","type":"openai","base_url":"https://upstream.example/v1","models":["model-a"]}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NoError(t, model.DB.Where("name = ?", "ch-map").First(&stored).Error)
	assert.JSONEq(t, `{"429":"400"}`, stored.GetStatusCodeMapping(), "省略 status_code_mapping 必须保持原值")

	// 显式 {} 清空。
	recorder = putChannelJSON(t, "ch-map", "", channelStatusMappingBody(`{}`))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NoError(t, model.DB.Where("name = ?", "ch-map").First(&stored).Error)
	assert.Empty(t, stored.StatusCodeMappingMap())
}

func TestPutChannelRejectsInvalidStatusCodeMapping(t *testing.T) {
	setupAPITestDB(t)

	for _, mapping := range []string{`{"429":"503"}`, `{"429":"200"}`, `{"bad":"400"}`, `{"429":"not-a-code"}`} {
		recorder := putChannelJSON(t, "ch-map", "", channelStatusMappingBody(mapping))
		require.Equal(t, http.StatusBadRequest, recorder.Code, "mapping=%s body=%s", mapping, recorder.Body.String())
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
		assert.Equal(t, "validation_failed", envelope.Error.Code)
	}

	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("name = ?", "ch-map").Count(&count).Error)
	assert.Zero(t, count, "非法映射不得落库")
}

// 导出必须带上映射：否则“导出→清空→导入”会悄悄丢掉对外状态码映射。
func TestBuildConfigBundleExportsStatusCodeMapping(t *testing.T) {
	setupAPITestDB(t)

	recorder := putChannelJSON(t, "ch-map", "", channelStatusMappingBody(`{"429":"400"}`))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	bundle, err := BuildConfigBundle()
	require.NoError(t, err)
	require.Len(t, bundle.Channels, 1)
	assert.Equal(t, map[string]string{"429": "400"}, bundle.Channels[0].StatusCodeMapping)
}
