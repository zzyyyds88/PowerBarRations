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

// 模型状态口径（api-spec §6.13，2026-10-25 起）：延迟 = 成功请求 p50/p95
// （样本 <5 → 0，total_ms 含重试链不再取均值）；吞吐量只算干净样本
// （成功且 total_attempts=1），多尝试请求不计入。
func TestModelStatusLatencyPercentilesAndCleanTPS(t *testing.T) {
	db := setupAPITestDB(t)
	now := time.Now().Unix()

	// 模型 pct-m：6 条单尝试成功（100~600ms）+ 1 条单尝试超长成功（288000ms）
	// + 1 条 3 连重试成功（288000ms，计分位不计 tps）+ 1 条失败（不进分位）。
	for _, ms := range []int64{100, 200, 300, 400, 500, 600, 288000} {
		require.NoError(t, db.Create(&model.PBRRequestLog{
			Ts: now - 60, UpstreamModel: "pct-m", Success: true,
			TotalMs: ms, TotalAttempts: 1, CompletionTokens: 1000, PromptTokens: 10,
		}).Error)
	}
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: now - 60, UpstreamModel: "pct-m", Success: true,
		TotalMs: 288000, TotalAttempts: 3, CompletionTokens: 6000,
	}).Error)
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: now - 60, UpstreamModel: "pct-m", Success: false, TotalMs: 999999,
	}).Error)

	rows := modelStatusRows(now-3600, now+1, "model")
	require.Len(t, rows, 1)
	row := rows[0]
	assert.EqualValues(t, 9, row["requests"], "7 单尝试成功 + 1 多尝试成功 + 1 失败")
	// 成功样本 8 条 [100..600, 288000×2]：p50 = 500（上中位，idx=int(0.5×8)=4）——不被长尾拉爆
	// （这正是废除均值的原因：均值会被 288s 的请求拉到 ~37s）；p95 = 288000，
	// 长尾如实占据 95 分位（分位数的诚实性：不藏尾巴，只是不让它污染中位）。
	assert.EqualValues(t, 500, row["p50_total_ms"])
	assert.EqualValues(t, 288000, row["p95_total_ms"])
	// 干净样本 = 7 条单尝试成功；多尝试的 288000/3 次请求不计入 tps。
	assert.EqualValues(t, 7, row["tps_samples"], "多尝试请求不计入 tps 样本")
	assert.Greater(t, row["output_tps"].(float64), 0.0)
	_, hasAvg := row["avg_total_ms"]
	assert.False(t, hasAvg, "avg_total_ms 已移除")

	// 样本 <5：分位为 0（界面显示 `-`，不拿噪声冒充指标）。
	require.NoError(t, db.Where("upstream_model = ?", "pct-m").Delete(&model.PBRRequestLog{}).Error)
	for _, ms := range []int64{100, 200, 300} {
		require.NoError(t, db.Create(&model.PBRRequestLog{
			Ts: now - 60, UpstreamModel: "pct-m", Success: true, TotalMs: ms, TotalAttempts: 1,
		}).Error)
	}
	small := modelStatusRows(now-3600, now+1, "model")
	require.Len(t, small, 1)
	assert.EqualValues(t, 0, small[0]["p50_total_ms"])
	assert.EqualValues(t, 0, small[0]["p95_total_ms"])

	// 嵌入类（无输出 token）：样本有效但 tps=0（tokens_out=0，界面显示 `-`）。
	require.NoError(t, db.Where("upstream_model = ?", "pct-m").Delete(&model.PBRRequestLog{}).Error)
	for i := 0; i < 6; i++ {
		require.NoError(t, db.Create(&model.PBRRequestLog{
			Ts: now - 60, UpstreamModel: "pct-embed", Success: true,
			TotalMs: 300, TotalAttempts: 1, PromptTokens: 100,
		}).Error)
	}
	embed := modelStatusRows(now-3600, now+1, "model")
	require.Len(t, embed, 1)
	assert.EqualValues(t, 6, embed[0]["tps_samples"])
	assert.EqualValues(t, 0.0, embed[0]["output_tps"])
}

// 首页「模型健康」按对外路由键聚合（group_by=lane）：只暴露用户请求里的 model，
// 不出现上游真名。默认 group_by=model 的既有契约（§6.13）保持不变。
func TestModelStatusGroupsByLaneWhenRequested(t *testing.T) {
	db := setupAPITestDB(t)
	now := time.Now().Unix()
	// 同一个对外车道 lane-out 下两个不同上游真名。
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: now, LaneName: "lane-out", MemberChannelName: "ch-a", UpstreamModel: "upstream-x",
		Success: true, TotalMs: 400, TotalAttempts: 1, CompletionTokens: 100,
	}).Error)
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: now, LaneName: "lane-out", MemberChannelName: "ch-b", UpstreamModel: "upstream-y",
		Success: true, TotalMs: 600, TotalAttempts: 1, CompletionTokens: 100,
	}).Error)

	rows := modelStatusRows(now-3600, now+1, "lane")
	require.Len(t, rows, 1, "两个上游真名应合并为同一条对外模型")
	assert.Equal(t, "lane-out", rows[0]["model"])
	assert.EqualValues(t, 2, rows[0]["requests"])

	byModel := modelStatusRows(now-3600, now+1, "model")
	require.Len(t, byModel, 2, "默认维度仍按上游真名分开")
}

// group_by 白名单：未知取值必须 400，不得静默回落。
func TestModelStatusRejectsUnknownGroupBy(t *testing.T) {
	setupAPITestDB(t)
	recorder := callAPI(t, http.MethodGet, "/api/v1/model-status?group_by=channel", "", GetModelStatus, gin.Params{})
	assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
}
