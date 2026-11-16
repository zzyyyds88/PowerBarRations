package model

import (
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/setting/pricing_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 计价键口径（design §16.9#7，2026-10-25 起）：渠道 × 成员所选模型。
// 旧口径按请求模型名（车道名）匹配，多车道部署下单价表永不命中、成本恒 0。

func TestEstimateRequestCostPricedByMemberModel(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	setting := `{"pbr_prices":[{"model":"model-priced","input":1,"output":2,"cache_read":0.1}]}`
	channel := &Channel{Name: "ch-price-key", Type: 1, Status: 1, Group: "default",
		Models: "model-priced", Setting: &setting}
	require.NoError(t, DB.Create(channel).Error)
	InitChannelCache()
	t.Cleanup(func() { InitChannelCache() })

	// 价格行没有 bands：任何时刻都走行级价，band 恒 -1。
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	costOf := func(channelId int, priceModel string, prompt, completion, cacheRead, cacheWrite int, semantic string) (float64, int) {
		return estimateRequestCost(channelId, priceModel, at, prompt, completion, cacheRead, cacheWrite, semantic)
	}

	// OpenAI 语义（默认）：缓存命中从 prompt 扣减——
	// 1M prompt（其中 1M 缓存读）全价输入 0 + 1M 输出×2 + 1M 缓存读×0.1 = 2.1 元。
	cost, band := costOf(channel.Id, "model-priced", 1_000_000, 1_000_000, 1_000_000, 0, "openai")
	assert.InDelta(t, 2.1, cost, 1e-9, "按成员所选模型命中单价表且缓存不重复计费")
	assert.Equal(t, pricing_setting.FallbackPriceBand, band, "无峰谷配置 → 兜底 -1")
	// Anthropic 语义：prompt 全额计输入价——1M×1 + 1M×2 + 1M×0.1 = 3.1 元。
	cost, _ = costOf(channel.Id, "model-priced", 1_000_000, 1_000_000, 1_000_000, 0, "anthropic")
	assert.InDelta(t, 3.1, cost, 1e-9)

	// 计价键不是车道名：请求模型名/车道名匹配不到单价表 → 0（不折算）。
	cost, _ = costOf(channel.Id, "some-lane-name", 1_000_000, 1_000_000, 0, 0, "openai")
	assert.InDelta(t, 0.0, cost, 1e-9, "车道名不是计价键")
	// 未配价的模型 → 0。
	cost, _ = costOf(channel.Id, "model-unpriced", 1_000_000, 1_000_000, 0, 0, "openai")
	assert.InDelta(t, 0.0, cost, 1e-9)
	// 渠道不存在 → 0。
	cost, band = costOf(99999, "model-priced", 1_000_000, 1_000_000, 0, 0, "openai")
	assert.InDelta(t, 0.0, cost, 1e-9)
	assert.Equal(t, pricing_setting.FallbackPriceBand, band)
}
