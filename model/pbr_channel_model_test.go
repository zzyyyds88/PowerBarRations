package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// W9：小时聚合必须写入「渠道 × 模型」维度，键为 渠道␟模型（breakdown W9）。
func TestHourlyAggregateStoresChannelModelBucket(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)

	entry := &PBRRequestLog{
		Ts:                1_700_000_000,
		LaneName:          "lane-cm",
		MemberChannelName: "channel-cm",
		TokenName:         "key-cm",
		RequestModel:      "model-cm",
		Success:           true,
		PromptTokens:      7,
		CompletionTokens:  9,
		EstimatedCost:     0.42,
	}
	upsertPBRHourlyStat(entry)

	var row PBRStatsHourly
	require.NoError(t, DB.Where(
		"group_kind = ? AND group_key = ?",
		"channel_model",
		"channel-cm"+PBRChannelModelSeparator+"model-cm",
	).First(&row).Error)
	assert.EqualValues(t, 1, row.Requests)
	assert.EqualValues(t, 1, row.Successes)
	assert.EqualValues(t, 7, row.PromptTokens)
	assert.EqualValues(t, 9, row.CompletionTokens)
	assert.InDelta(t, 0.42, row.CostSum, 1e-9)

	// 渠道或模型为空时不写 channel_model 桶。
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)
	upsertPBRHourlyStat(&PBRRequestLog{Ts: 1_700_000_000, MemberChannelName: "", RequestModel: "model-cm"})
	upsertPBRHourlyStat(&PBRRequestLog{Ts: 1_700_000_000, MemberChannelName: "channel-cm", RequestModel: "   "})
	var emptyCount int64
	require.NoError(t, DB.Model(&PBRStatsHourly{}).Where("group_kind = ?", "channel_model").Count(&emptyCount).Error)
	assert.Zero(t, emptyCount, "渠道/模型为空不得写 channel_model 桶")

	// AggregatePBRStatsFromHourly 支持按 channel_model 读回该桶，且不串味其它维度。
	upsertPBRHourlyStat(entry)
	byChannelModel, err := AggregatePBRStatsFromHourly(0, 0, "hour", "channel_model")
	require.NoError(t, err)
	require.Len(t, byChannelModel, 1)
	assert.Equal(t, "channel-cm"+PBRChannelModelSeparator+"model-cm", byChannelModel[0].GroupKey)
	assert.EqualValues(t, 1, byChannelModel[0].Requests)

	byLane, err := AggregatePBRStatsFromHourly(0, 0, "hour", "lane")
	require.NoError(t, err)
	require.Len(t, byLane, 1)
	assert.Equal(t, "lane-cm", byLane[0].GroupKey)
}

// W9：一次性回填从明细聚合出「渠道 × 模型」桶，且重复调用幂等。
func TestBackfillChannelModelStatsAggregatesDetailAndIsIdempotent(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM options").Error)

	baseTs := int64(1_700_000_000)
	details := []*PBRRequestLog{
		{Ts: baseTs, MemberChannelName: "ch-a", RequestModel: "m-a", Success: true, PromptTokens: 10, CompletionTokens: 20, EstimatedCost: 0.5},
		{Ts: baseTs + 60, MemberChannelName: "ch-a", RequestModel: "m-a", Success: false, PromptTokens: 5, CompletionTokens: 0, EstimatedCost: 0.1},
		{Ts: baseTs + 120, MemberChannelName: "ch-a", RequestModel: "m-b", Success: true, PromptTokens: 1, CompletionTokens: 2, EstimatedCost: 0.2},
	}
	for _, detail := range details {
		require.NoError(t, DB.Create(detail).Error)
	}

	require.NoError(t, BackfillPBRChannelModelStats())

	var first []PBRStatsHourly
	require.NoError(t, DB.Where("group_kind = ?", "channel_model").Order("group_key").Find(&first).Error)
	require.Len(t, first, 2)

	var bucketMA PBRStatsHourly
	require.NoError(t, DB.Where(
		"group_kind = ? AND group_key = ?",
		"channel_model",
		"ch-a"+PBRChannelModelSeparator+"m-a",
	).First(&bucketMA).Error)
	assert.EqualValues(t, 2, bucketMA.Requests)
	assert.EqualValues(t, 1, bucketMA.Successes)
	assert.EqualValues(t, 15, bucketMA.PromptTokens)
	assert.EqualValues(t, 20, bucketMA.CompletionTokens)
	assert.InDelta(t, 0.6, bucketMA.CostSum, 1e-9)

	var bucketMB PBRStatsHourly
	require.NoError(t, DB.Where(
		"group_kind = ? AND group_key = ?",
		"channel_model",
		"ch-a"+PBRChannelModelSeparator+"m-b",
	).First(&bucketMB).Error)
	assert.EqualValues(t, 1, bucketMB.Requests)

	// 成功后写入 option 标记，避免重放。
	var marker Option
	require.NoError(t, DB.Where(&Option{Key: pbrChannelModelBackfillOptionKey}).First(&marker).Error)

	// 第二次调用必须是 no-op（幂等，不叠加）。
	require.NoError(t, BackfillPBRChannelModelStats())
	var second []PBRStatsHourly
	require.NoError(t, DB.Where("group_kind = ?", "channel_model").Order("group_key").Find(&second).Error)
	assert.Equal(t, first, second, "重复回填不得改变聚合桶")

	var total int64
	require.NoError(t, DB.Model(&PBRStatsHourly{}).
		Where("group_kind = ?", "channel_model").
		Select("COALESCE(SUM(requests),0)").Scan(&total).Error)
	assert.EqualValues(t, 3, total, "channel_model 合计应等于明细条数")
}

// 已存在的桶（例如新代码已实时写入）不得被回填覆盖或叠加。
func TestBackfillChannelModelStatsPreservesExistingBucket(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM options").Error)

	baseTs := int64(1_700_000_000)
	// 实时写入用的是小时取整桶，预先存在的桶必须落在同一桶上才能验证 DoNothing。
	bucketTs := (baseTs / 3600) * 3600
	require.NoError(t, DB.Create(&PBRRequestLog{
		Ts: baseTs, MemberChannelName: "ch-b", RequestModel: "m-a", Success: true, PromptTokens: 3, EstimatedCost: 0.3,
	}).Error)
	// 预先存在一个实时写入的桶（模拟崩溃后未写标记的场景）：回填不得改动它。
	require.NoError(t, DB.Create(&PBRStatsHourly{
		BucketTs: bucketTs, GroupKind: "channel_model",
		GroupKey: "ch-b" + PBRChannelModelSeparator + "m-a",
		Requests: 5, Successes: 5, PromptTokens: 50, CostSum: 5,
	}).Error)

	require.NoError(t, BackfillPBRChannelModelStats())

	var row PBRStatsHourly
	require.NoError(t, DB.Where(
		"group_kind = ? AND group_key = ?",
		"channel_model",
		"ch-b"+PBRChannelModelSeparator+"m-a",
	).First(&row).Error)
	assert.EqualValues(t, 5, row.Requests, "已存在的桶必须原样保留，不能被回填覆盖")
	assert.InDelta(t, 5, row.CostSum, 1e-9)
}

// 明细为空时跳过回填：不产生桶，也不报错。
func TestBackfillChannelModelStatsSkipsWithoutDetail(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM options").Error)

	require.NoError(t, BackfillPBRChannelModelStats())

	var count int64
	require.NoError(t, DB.Model(&PBRStatsHourly{}).Where("group_kind = ?", "channel_model").Count(&count).Error)
	assert.Zero(t, count, "没有明细时不应产生 channel_model 桶")
}
