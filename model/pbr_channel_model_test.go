package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 小时聚合与重建（api-spec §5.5）：「模型」维度口径 = **上游真实模型名**
// （upstream_model，2026-10-25 起）；channel_model / key_model 同口径；rebuild
// 幂等并按当前单价重折算成本。

func TestHourlyAggregateStoresModelBuckets(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)

	entry := &PBRRequestLog{
		Ts:                1_700_000_000,
		LaneName:          "lane-cm",
		MemberChannelName: "channel-cm",
		TokenName:         "key-cm",
		// 池化车道：请求模型（车道名）与真实上游模型不同——"模型"维度必须用后者。
		RequestModel:     "lane-cm",
		UpstreamModel:    "model-cm",
		MemberModel:      "model-cm",
		Success:          true,
		PromptTokens:     7,
		CompletionTokens: 9,
		EstimatedCost:    0.42,
	}
	upsertPBRHourlyStat(entry)

	var row PBRStatsHourly
	require.NoError(t, DB.Where(
		"group_kind = ? AND group_key = ?",
		"channel_model",
		"channel-cm"+PBRChannelModelSeparator+"model-cm",
	).First(&row).Error, "channel_model 键 = 渠道␟upstream_model（不是车道名）")
	assert.EqualValues(t, 1, row.Requests)
	assert.EqualValues(t, 1, row.Successes)
	assert.EqualValues(t, 7, row.PromptTokens)
	assert.EqualValues(t, 9, row.CompletionTokens)
	assert.InDelta(t, 0.42, row.CostSum, 1e-9)

	var modelRow PBRStatsHourly
	require.NoError(t, DB.Where(
		"group_kind = ? AND group_key = ?",
		"model", "model-cm",
	).First(&modelRow).Error, "model 维度按 upstream_model 聚合")
	var keyModelRow PBRStatsHourly
	require.NoError(t, DB.Where(
		"group_kind = ? AND group_key = ?",
		"key_model", "key-cm"+PBRChannelModelSeparator+"model-cm",
	).First(&keyModelRow).Error, "key_model = 令牌␟upstream_model")

	// lane 维度不受影响（车道名口径）。
	byLane, err := AggregatePBRStatsFromHourly(0, 0, "hour", "lane")
	require.NoError(t, err)
	require.Len(t, byLane, 1)
	assert.Equal(t, "lane-cm", byLane[0].GroupKey)

	// upstream_model 为空（管理动作等）不写 model 系维度；组合维度缺任一成分不写。
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)
	upsertPBRHourlyStat(&PBRRequestLog{Ts: 1_700_000_000, MemberChannelName: "c", UpstreamModel: ""})
	upsertPBRHourlyStat(&PBRRequestLog{Ts: 1_700_000_000, MemberChannelName: "", UpstreamModel: "m"})
	upsertPBRHourlyStat(&PBRRequestLog{Ts: 1_700_000_000, MemberChannelName: "c", UpstreamModel: "m", TokenName: ""})
	// 记录2/3 各累计一条 model="m"（同桶 upsert 合并为一行 requests=2）；
	// channel_model 只在渠道+模型都非空时写（记录3）；key_model 需要 token 非空——
	// 三条都没有，恒 0。
	sumKind := func(kind string) int64 {
		var n int64
		require.NoError(t, DB.Model(&PBRStatsHourly{}).Where("group_kind = ?", kind).
			Select("COALESCE(SUM(requests),0)").Scan(&n).Error)
		return n
	}
	assert.EqualValues(t, 2, sumKind("model"), "upstream_model 非空即写 model 维度")
	assert.EqualValues(t, 1, sumKind("channel_model"), "渠道缺失的组合维度不写")
	assert.EqualValues(t, 0, sumKind("key_model"), "令牌缺失不写 key_model")
}

// RebuildStatsFromLogs：dry-run 不落库；实跑重建聚合（新口径）并重折算成本；幂等。
func TestRebuildStatsRebuildsAndIsIdempotent(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_stats_hourly").Error)
	require.NoError(t, DB.Exec("DELETE FROM pbr_request_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, DB.Exec("DELETE FROM options").Error)

	// 渠道配单价表：model-priced 输入 1 元/M、输出 2 元/M（计价键 = 渠道 × 模型）。
	channel := newTestChannel(t, "ch-price", "model-priced")
	setting := `{"pbr_prices":[{"model":"model-priced","input":1,"output":2}]}`
	channel.Setting = &setting
	require.NoError(t, DB.Save(channel).Error)
	InitChannelCache()

	baseTs := int64(1_700_000_000)
	// 一条旧明细：estimated_cost=0（旧口径计价键错位永不命中），有 member_model。
	oldEntry := &PBRRequestLog{
		Ts: baseTs, LaneName: "lane-r", MemberChannelName: "ch-price", MemberChannelId: channel.Id,
		RequestModel: "lane-r", UpstreamModel: "model-priced", MemberModel: "model-priced",
		TokenName: "key-r", Success: true, PromptTokens: 1_000_000, CompletionTokens: 1_000_000,
		EstimatedCost: 0,
	}
	// 一条历史明细（member_model 为空）：折算键回退 upstream_model。
	legacyEntry := &PBRRequestLog{
		Ts: baseTs + 60, MemberChannelName: "ch-price", MemberChannelId: channel.Id,
		UpstreamModel: "model-priced", TokenName: "key-r", Success: true,
		PromptTokens: 500_000, CompletionTokens: 0, EstimatedCost: 0,
	}
	// 一条 Anthropic 语义明细：prompt 全额计输入价（缓存不扣减）。
	anthropicEntry := &PBRRequestLog{
		Ts: baseTs + 120, MemberChannelName: "ch-price", MemberChannelId: channel.Id,
		UpstreamModel: "model-priced", MemberModel: "model-priced", UsageSemantic: "anthropic",
		TokenName: "key-r", Success: true,
		PromptTokens: 1_000_000, CacheReadTokens: 1_000_000, EstimatedCost: 0,
	}
	for _, e := range []*PBRRequestLog{oldEntry, legacyEntry, anthropicEntry} {
		require.NoError(t, DB.Create(e).Error)
	}

	// dry-run：报告差异但不落库。
	preview, err := RebuildStatsFromLogs(true)
	require.NoError(t, err)
	assert.True(t, preview.DryRun)
	assert.Equal(t, preview.CostSumBefore, float64(0))
	// 本测试的单价表只有 input=1/output=2（cache_read 未填 = 按 0 计）：
	// openai 条 1M in×1 + 1M out×2 = 3.0 元（无缓存行，扣减不生效）；
	// legacy 0.5M in ×1 = 0.5 元；anthropic 条 1M×1（不扣缓存）+ 缓存按 0 = 1.0 元；合计 4.5 元。
	assert.InDelta(t, 4.5, preview.CostSumAfter, 1e-9)
	assert.EqualValues(t, 3, preview.UpdatedLogs)
	var staleCost PBRRequestLog
	require.NoError(t, DB.First(&staleCost, oldEntry.Id).Error)
	assert.InDelta(t, 0, staleCost.EstimatedCost, 1e-9, "dry-run 不得改动明细")

	// 实跑：明细成本被重折算、聚合表按新口径重建。
	result, err := RebuildStatsFromLogs(false)
	require.NoError(t, err)
	assert.InDelta(t, 4.5, result.CostSumAfter, 1e-9)
	var updated PBRRequestLog
	require.NoError(t, DB.First(&updated, oldEntry.Id).Error)
	assert.InDelta(t, 3.0, updated.EstimatedCost, 1e-9, "按当前单价表重折算（渠道 × 模型）")
	var updatedAnthropic PBRRequestLog
	require.NoError(t, DB.First(&updatedAnthropic, anthropicEntry.Id).Error)
	t.Logf("anthropic row: prompt=%d cache_r=%d semantic=%q cost=%.4f", updatedAnthropic.PromptTokens, updatedAnthropic.CacheReadTokens, updatedAnthropic.UsageSemantic, updatedAnthropic.EstimatedCost)
	assert.InDelta(t, 1.0, updatedAnthropic.EstimatedCost, 1e-9, "anthropic 语义不扣减缓存（本测试单价未填 cache_read，按 0 计）")

	var modelBucket PBRStatsHourly
	require.NoError(t, DB.Where("group_kind = ? AND group_key = ?",
		"model", "model-priced").First(&modelBucket).Error)
	assert.EqualValues(t, 3, modelBucket.Requests)
	assert.InDelta(t, 4.5, modelBucket.CostSum, 1e-9, "聚合按重折算后的成本")
	var keyModelBucket PBRStatsHourly
	require.NoError(t, DB.Where("group_kind = ? AND group_key = ?",
		"key_model", "key-r"+PBRChannelModelSeparator+"model-priced").First(&keyModelBucket).Error)

	// 幂等：再跑一遍结果一致。
	again, err := RebuildStatsFromLogs(false)
	require.NoError(t, err)
	assert.InDelta(t, result.CostSumAfter, again.CostSumAfter, 1e-9)
	assert.Equal(t, result.AggregateRows, again.AggregateRows)
	assert.EqualValues(t, 0, again.UpdatedLogs, "第二次没有明细需要更新")
	var total int64
	require.NoError(t, DB.Model(&PBRStatsHourly{}).
		Where("group_kind = ?", "model").Select("COALESCE(SUM(requests),0)").Scan(&total).Error)
	assert.EqualValues(t, 3, total, "重复重建不得叠加")
}
