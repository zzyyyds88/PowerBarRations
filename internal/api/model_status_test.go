package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 模型状态（api-spec §6.13）：跨渠道按上游模型聚合，含输出速度口径。
func TestModelStatusAggregatesByModelWithSpeed(t *testing.T) {
	db := setupAPITestDB(t)
	now := time.Now().Unix()
	// 同一模型：一条流式成功（ttft 100ms、总 1100ms、输出 1000 token）
	// 与一条非流式成功（总 500ms、输出 0 token）。
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: now, MemberChannelName: "ch-a", UpstreamModel: "m-1", Success: true,
		IsStream: true, TTFTMs: 100, TotalMs: 1100, CompletionTokens: 1000, PromptTokens: 200,
	}).Error)
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: now, MemberChannelName: "ch-b", UpstreamModel: "m-1", Success: true,
		IsStream: false, TotalMs: 500, CompletionTokens: 0, PromptTokens: 100,
	}).Error)
	// 另一个模型：一条失败。
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: now, MemberChannelName: "ch-a", UpstreamModel: "m-2", Success: false,
		IsStream: false, TotalMs: 300, HTTPStatus: 500,
	}).Error)

	recorder := callAPI(t, http.MethodGet, "/api/v1/model-status", "", GetModelStatus, gin.Params{})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp struct {
		From  int64 `json:"from"`
		To    int64 `json:"to"`
		Items []struct {
			Model       string  `json:"model"`
			Requests    int64   `json:"requests"`
			Successes   int64   `json:"successes"`
			Failures    int64   `json:"failures"`
			SuccessRate float64 `json:"success_rate"`
			AvgTTFTMs   float64 `json:"avg_ttft_ms"`
			OutputTPS   float64 `json:"output_tps"`
			TokensOut   int64   `json:"tokens_out"`
			TokensTotal int64   `json:"tokens_total"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.Positive(t, resp.From, "省略 from 时应回落到最近 24 小时")
	assert.Positive(t, resp.To)
	require.Len(t, resp.Items, 2)
	// 按 tokens_out 降序：m-1 在前。
	assert.Equal(t, "m-1", resp.Items[0].Model)
	assert.EqualValues(t, 2, resp.Items[0].Requests)
	assert.EqualValues(t, 2, resp.Items[0].Successes)
	assert.InDelta(t, 1.0, resp.Items[0].SuccessRate, 1e-9)
	assert.InDelta(t, 100.0, resp.Items[0].AvgTTFTMs, 1e-6, "首字延迟只对流式有样本")
	// 生成耗时 = (1100-100) + 500 = 1500ms，输出 1000 token → 666.7 tps。
	assert.InDelta(t, 1000.0*1000/1500.0, resp.Items[0].OutputTPS, 1e-6)
	// tokens_total = 输入(200+100) + 输出(1000+0) = 1300。
	assert.EqualValues(t, 1300, resp.Items[0].TokensTotal)

	assert.Equal(t, "m-2", resp.Items[1].Model)
	assert.EqualValues(t, 1, resp.Items[1].Failures)
	assert.InDelta(t, 0.0, resp.Items[1].SuccessRate, 1e-9)
}
