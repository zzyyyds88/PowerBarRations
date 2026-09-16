package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /api/stats 的数据源必须是小时聚合表：明细被 prune 删掉后历史统计仍然存在
// （design-v1 §16.5，审查 F5）。此前 /stats 读明细表，一次清理就让历史凭空消失，
// 而聚合表只写不读、成了死数据。
func TestStatsSurviveDetailPrune(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)

	baseTs := int64(1_700_000_000)
	for i := 0; i < 3; i++ {
		entry := &PBRRequestLog{
			Ts:                baseTs + int64(i),
			LaneName:          "lane-prune",
			MemberChannelName: "channel-prune",
			TokenName:         "key-prune",
			RequestModel:      "model-prune",
			Success:           i != 0,
			PromptTokens:      10,
			CompletionTokens:  20,
			EstimatedCost:     0.25,
		}
		require.NoError(t, DB.Create(entry).Error)
		upsertPBRHourlyStat(entry)
	}

	before, err := AggregatePBRStatsFromHourly(0, 0, "hour", "lane")
	require.NoError(t, err)
	require.Len(t, before, 1)
	assert.EqualValues(t, 3, before[0].Requests)
	assert.EqualValues(t, 2, before[0].Successes)
	assert.EqualValues(t, 30, before[0].PromptTokens)
	assert.InDelta(t, 0.75, before[0].EstimatedCost, 1e-9)

	// 明细按保留策略清理：聚合统计不得变化。
	deleted, err := PrunePBRRequestLogsBefore(baseTs + 100)
	require.NoError(t, err)
	assert.EqualValues(t, 3, deleted)

	after, err := AggregatePBRStatsFromHourly(0, 0, "hour", "lane")
	require.NoError(t, err)
	require.Len(t, after, 1, "prune 后聚合桶必须还在")
	assert.Equal(t, before[0], after[0], "prune 不得改变聚合统计")

	// 明细口径确实已经空了（证明这次断言不是"两边都没数据"）。
	detail, err := AggregatePBRStats(0, 0, "hour", "lane")
	require.NoError(t, err)
	assert.Empty(t, detail, "明细已清空，这正是读明细会丢历史的原因")
}

// day 粒度由小时桶上卷，且时间过滤按桶起点；不同维度互不串味。
func TestAggregateStatsFromHourlyRollsUpAndFilters(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)

	day1 := int64(1_700_000_000) // 2023-11-14T22:13:20Z
	day2 := day1 + 86400
	entries := []*PBRRequestLog{
		{Ts: day1, LaneName: "lane-x", MemberChannelName: "ch-x", TokenName: "key-x", RequestModel: "model-x", Success: true},
		{Ts: day1 + 3600, LaneName: "lane-x", MemberChannelName: "ch-x", TokenName: "key-x", RequestModel: "model-x", Success: false},
		{Ts: day2, LaneName: "lane-y", MemberChannelName: "ch-y", TokenName: "key-y", RequestModel: "model-y", Success: true},
	}
	for _, entry := range entries {
		upsertPBRHourlyStat(entry)
	}

	hourly, err := AggregatePBRStatsFromHourly(0, 0, "hour", "lane")
	require.NoError(t, err)
	assert.Len(t, hourly, 3, "三个小时桶")

	daily, err := AggregatePBRStatsFromHourly(0, 0, "day", "lane")
	require.NoError(t, err)
	require.Len(t, daily, 2, "两个天桶（上卷）")
	assert.Equal(t, day2/86400*86400, daily[0].BucketTs, "按天取整并倒序")
	assert.EqualValues(t, 1, daily[0].Requests)
	assert.EqualValues(t, 2, daily[1].Requests)

	// 维度隔离：channel 维度不会混进 lane 的数据。
	byChannel, err := AggregatePBRStatsFromHourly(0, 0, "hour", "channel")
	require.NoError(t, err)
	keys := make([]string, 0, len(byChannel))
	for _, bucket := range byChannel {
		keys = append(keys, bucket.GroupKey)
	}
	assert.ElementsMatch(t, []string{"ch-y", "ch-x", "ch-x"}, keys)

	// 时间过滤按桶起点。
	filtered, err := AggregatePBRStatsFromHourly(day2, 0, "hour", "lane")
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, "lane-y", filtered[0].GroupKey)

	// 非法 group_by 回落到 lane，而不是报错或返回空。
	fallback, err := AggregatePBRStatsFromHourly(0, 0, "hour", "nonsense")
	require.NoError(t, err)
	assert.Len(t, fallback, 3)
}

// 空表返回空切片而不是 nil（JSON 契约要求 items 是数组）。
func TestAggregateStatsFromHourlyEmptyIsEmptySlice(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)

	buckets, err := AggregatePBRStatsFromHourly(0, 0, "hour", "lane")
	require.NoError(t, err)
	assert.NotNil(t, buckets)
	assert.Empty(t, buckets)
}
