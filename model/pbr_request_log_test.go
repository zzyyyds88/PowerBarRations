package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 源码审计修复的回归用例（日志与聚合）。
//
// 首插必须带本次增量：只靠 ON CONFLICT 的 += 会让每个新维度组恒定少 1 条
// （实测 3000 请求下 lane 维度合计 2997）。
func TestHourlyAggregateCountsFirstRequest(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)

	entry := &PBRRequestLog{
		Ts:                1_700_000_000,
		LaneName:          "lane-agg",
		MemberChannelName: "channel-agg",
		TokenName:         "key-agg",
		RequestModel:      "model-agg",
		Success:           true,
		PromptTokens:      11,
		CompletionTokens:  22,
		EstimatedCost:     0.5,
	}
	upsertPBRHourlyStat(entry)

	var row PBRStatsHourly
	require.NoError(t, DB.Where("group_kind = ? AND group_key = ?", "lane", "lane-agg").First(&row).Error)
	assert.EqualValues(t, 1, row.Requests, "首插就要记 1，而不是 0")
	assert.EqualValues(t, 1, row.Successes)
	assert.EqualValues(t, 11, row.PromptTokens)
	assert.EqualValues(t, 22, row.CompletionTokens)
	assert.InDelta(t, 0.5, row.CostSum, 1e-9)

	// 再来两条：应累加到 3，且只有一个维度行
	upsertPBRHourlyStat(entry)
	upsertPBRHourlyStat(entry)
	var total int64
	require.NoError(t, DB.Model(&PBRStatsHourly{}).
		Where("group_kind = ? AND group_key = ?", "lane", "lane-agg").
		Select("COALESCE(SUM(requests),0)").Scan(&total).Error)
	assert.EqualValues(t, 3, total)
}

// 聚合维度数与明细行数的关系：lane 维度合计必须等于明细条数。
func TestHourlyAggregateMatchesDetailForLaneDimension(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)

	for i := 0; i < 5; i++ {
		entry := &PBRRequestLog{
			Ts:                1_700_000_000 + int64(i),
			LaneName:          "lane-detail",
			MemberChannelName: "channel-detail",
			TokenName:         "key-detail",
			RequestModel:      "model-detail",
			Success:           true,
		}
		upsertPBRHourlyStat(entry)
	}
	var total int64
	require.NoError(t, DB.Model(&PBRStatsHourly{}).
		Where("group_kind = ?", "lane").
		Select("COALESCE(SUM(requests),0)").Scan(&total).Error)
	assert.EqualValues(t, 5, total, "lane 维度合计必须等于请求数（少 1 说明首插没记增量）")
}
