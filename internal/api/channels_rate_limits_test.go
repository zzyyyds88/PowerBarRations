package api

// 渠道出站准入配置（ADR 0012）：rate_limits 写后回读、省略保持、[] 清空、
// 非法参数 400；导出/摘要必须带上，否则恢复实例会丢限流配置。

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func channelRateLimitsBody(rateLimits string) string {
	return `{"name":"ch-rl","type":"openai","base_url":"https://upstream.example/v1",` +
		`"models":["model-a"],"rate_limits":` + rateLimits + `}`
}

func TestPutChannelRateLimitsRoundTrip(t *testing.T) {
	setupAPITestDB(t)

	recorder := putChannelJSON(t, "ch-rl", "",
		channelRateLimitsBody(`[{"model":"model-a","max_concurrency":4,"max_rpm":12,"queue_wait_seconds":5}]`))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var response struct {
		RateLimits []struct {
			Model            string `json:"model"`
			MaxConcurrency   int    `json:"max_concurrency"`
			MaxRPM           int    `json:"max_rpm"`
			QueueLimit       int    `json:"queue_limit"`
			QueueWaitSeconds int    `json:"queue_wait_seconds"`
		} `json:"rate_limits"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.RateLimits, 1)
	assert.Equal(t, "model-a", response.RateLimits[0].Model)
	assert.Equal(t, 4, response.RateLimits[0].MaxConcurrency)
	assert.Equal(t, 12, response.RateLimits[0].MaxRPM)
	assert.Equal(t, 32, response.RateLimits[0].QueueLimit, "queue_limit=0 归一化为默认 32")
	assert.Equal(t, 5, response.RateLimits[0].QueueWaitSeconds)

	var stored model.Channel
	require.NoError(t, model.DB.Where("name = ?", "ch-rl").First(&stored).Error)
	require.Len(t, stored.GetSetting().PBRRateLimits, 1)

	// 省略 rate_limits = 保持原值。
	recorder = putChannelJSON(t, "ch-rl", "",
		`{"name":"ch-rl","type":"openai","base_url":"https://upstream.example/v1","models":["model-a"]}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NoError(t, model.DB.Where("name = ?", "ch-rl").First(&stored).Error)
	require.Len(t, stored.GetSetting().PBRRateLimits, 1)
	assert.Equal(t, 5, stored.GetSetting().PBRRateLimits[0].QueueWaitSeconds, "省略 rate_limits 必须保持原值")

	// 显式 [] 清空。
	recorder = putChannelJSON(t, "ch-rl", "", channelRateLimitsBody(`[]`))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NoError(t, model.DB.Where("name = ?", "ch-rl").First(&stored).Error)
	assert.Empty(t, stored.GetSetting().PBRRateLimits)

	// 导出带上（自适应 -1 也必须原样往返）。
	recorder = putChannelJSON(t, "ch-rl", "",
		channelRateLimitsBody(`[{"model":"model-a","max_concurrency":-1,"max_rpm":12}]`))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	bundle, err := BuildConfigBundle()
	require.NoError(t, err)
	require.Len(t, bundle.Channels, 1)
	require.Len(t, bundle.Channels[0].RateLimits, 1)
	assert.Equal(t, -1, bundle.Channels[0].RateLimits[0].MaxConcurrency)
}

func TestPutChannelRejectsInvalidRateLimits(t *testing.T) {
	setupAPITestDB(t)

	cases := []string{
		`[{"model":"","max_concurrency":1}]`,
		`[{"model":"m","max_concurrency":-2}]`,
		`[{"model":"m","max_rpm":-1}]`,
		`[{"model":"m","queue_limit":-1}]`,
		`[{"model":"m","queue_wait_seconds":-1}]`,
		`[{"model":"m"},{"model":"m"}]`,
	}
	for _, raw := range cases {
		recorder := putChannelJSON(t, "ch-rl", "", channelRateLimitsBody(raw))
		require.Equal(t, http.StatusBadRequest, recorder.Code, "rate_limits=%s body=%s", raw, recorder.Body.String())
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
		assert.Equal(t, "validation_failed", envelope.Error.Code)
	}

	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("name = ?", "ch-rl").Count(&count).Error)
	assert.Zero(t, count, "非法 rate_limits 不得落库")
}
