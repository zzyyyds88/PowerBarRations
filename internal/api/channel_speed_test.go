package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 渠道实时速度（api-spec §6.14）：覆盖全部配置渠道，空闲渠道零值；
// 口径与模型状态一致（干净样本 tps、成功请求分位、0~1 成功率）。
func TestChannelSpeedAggregatesPerChannel(t *testing.T) {
	db := setupAPITestDB(t)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.Channel{
		Name: "speed-a", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "m-a",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Name: "speed-idle", Type: 1, Key: "sk", Status: common.ChannelStatusManuallyDisabled,
		Group: "default", Models: "m-idle",
	}).Error)

	// speed-a：两条干净流式成功（输出 1000 token、生成 900ms + 100ms）
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: now, MemberChannelName: "speed-a", UpstreamModel: "m-a", Success: true,
		IsStream: true, TTFTMs: 100, TotalMs: 1000, CompletionTokens: 600, PromptTokens: 100,
		TotalAttempts: 1, EstimatedCost: 0.02,
	}).Error)
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: now, MemberChannelName: "speed-a", UpstreamModel: "m-a", Success: true,
		IsStream: true, TTFTMs: 50, TotalMs: 150, CompletionTokens: 400, PromptTokens: 100,
		TotalAttempts: 1, EstimatedCost: 0.01,
	}).Error)
	// 一条多次尝试的失败：计入请求数/成功率，但不计入速度。
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: now, MemberChannelName: "speed-a", UpstreamModel: "m-a", Success: false,
		TotalMs: 300, HTTPStatus: 500, TotalAttempts: 2,
	}).Error)

	recorder := callAPI(t, http.MethodGet, "/api/v1/channel-speed", "", GetChannelSpeed, gin.Params{})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp struct {
		From  int64             `json:"from"`
		To    int64             `json:"to"`
		Items []channelSpeedRow `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.Positive(t, resp.From, "省略 from 时应回落到最近 15 分钟")
	assert.Positive(t, resp.To)
	require.Len(t, resp.Items, 2, "空闲/停用渠道也必须出现")

	active := resp.Items[0]
	assert.Equal(t, "speed-a", active.Channel)
	assert.True(t, active.Enabled)
	assert.EqualValues(t, 3, active.Requests)
	assert.EqualValues(t, 2, active.Successes)
	assert.EqualValues(t, 1, active.Failures)
	assert.InDelta(t, 2.0/3.0, active.SuccessRate, 1e-9)
	// 生成耗时 = (1000-100) + (150-50) = 1000ms，输出 1000 token → 1000 tps。
	assert.InDelta(t, 1000.0, active.OutputTPS, 1e-6)
	assert.EqualValues(t, 2, active.TPSSamples)
	// 整体输出速度：窗口内全部 completion token / 窗口秒数（默认 15m）。
	assert.InDelta(t, 1000.0/float64(resp.To-resp.From), active.OutputRate, 1e-9)
	// 生成阶段聚合：两条流式干净样本生成 900ms + 100ms（失败请求 non-stream 300ms 也计入）。
	assert.InDelta(t, 1000.0*1000/(900+100+300), active.GenerationRate, 1e-6)
	assert.InDelta(t, 0.03, active.Cost, 1e-9)
	assert.EqualValues(t, 1000, active.TokensOut)
	assert.EqualValues(t, 200, active.TokensIn)
	assert.EqualValues(t, 1200, active.TokensTotal)
	// 样本 <5：分位数为 0（界面显示 -）。
	assert.Zero(t, active.P50TotalMs)
	assert.Zero(t, active.P95TotalMs)

	idle := resp.Items[1]
	assert.Equal(t, "speed-idle", idle.Channel)
	assert.False(t, idle.Enabled)
	assert.Zero(t, idle.Requests)
	assert.Zero(t, idle.OutputTPS)
	assert.Zero(t, idle.TPSSamples)
	assert.Zero(t, idle.OutputRate)
}

type channelSpeedRow struct {
	Channel        string  `json:"channel"`
	Enabled        bool    `json:"enabled"`
	Requests       int64   `json:"requests"`
	Successes      int64   `json:"successes"`
	Failures       int64   `json:"failures"`
	SuccessRate    float64 `json:"success_rate"`
	P50TotalMs     int64   `json:"p50_total_ms"`
	P95TotalMs     int64   `json:"p95_total_ms"`
	OutputTPS      float64 `json:"output_tps"`
	TPSSamples     int64   `json:"tps_samples"`
	OutputRate     float64 `json:"output_tokens_per_second"`
	GenerationRate float64 `json:"generation_tokens_per_second"`
	TokensIn       int64   `json:"tokens_in"`
	TokensOut      int64   `json:"tokens_out"`
	TokensTotal    int64   `json:"tokens_total"`
	Cost           float64 `json:"estimated_cost"`
}

// 已删除渠道的历史日志不得变成"幽灵渠道"。
func TestChannelSpeedExcludesRemovedChannels(t *testing.T) {
	db := setupAPITestDB(t)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.Channel{
		Name: "kept", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "m",
	}).Error)
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts: now, MemberChannelName: "ghost", UpstreamModel: "m", Success: true,
		IsStream: false, TotalMs: 100, CompletionTokens: 100, TotalAttempts: 1,
	}).Error)

	recorder := callAPI(t, http.MethodGet, "/api/v1/channel-speed", "", GetChannelSpeed, gin.Params{})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp struct {
		Items []channelSpeedRow `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	assert.Equal(t, "kept", resp.Items[0].Channel)
}

// from/to 非法时按管理面统一错误处理（400 validation_failed）。
func TestChannelSpeedRejectsBadTime(t *testing.T) {
	setupAPITestDB(t)
	recorder := callAPI(t, http.MethodGet, "/api/v1/channel-speed?from=yesterday", "", GetChannelSpeed, gin.Params{})
	assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
}
