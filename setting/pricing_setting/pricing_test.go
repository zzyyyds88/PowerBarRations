package pricing_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 渠道级上游单价只做成本折算展示（design-v1 §16.9#7）：这个用例锁住
// "人民币/百万 token" 的换算口径，避免以后有人把它接回扣费。
// 全局默认单价表已移除：单价只来自渠道（pbr_prices），未配价即不折算。

func TestEstimateWithPriceUsesPerMillionPricing(t *testing.T) {
	price := ModelPrice{Model: "m1", Input: 2.5, Output: 10, CacheRead: 1.25, CacheWrite: 3}

	// 1M 输入 + 1M 输出 + 1M 缓存读 + 1M 缓存写 = 2.5 + 10 + 1.25 + 3
	assert.InDelta(t, 16.75, EstimateWithPrice(price, 1_000_000, 1_000_000, 1_000_000, 1_000_000), 1e-9)
	// 按比例：100 万 token 单价就是数字本身，所以 1000 token = 千分之一
	assert.InDelta(t, 0.0025, EstimateWithPrice(price, 1000, 0, 0, 0), 1e-9)
	// 零价与空单价都折算为 0（不折算）
	assert.Equal(t, 0.0, EstimateWithPrice(ModelPrice{Model: "m2"}, 1_000_000, 1_000_000, 1_000_000, 1_000_000))
}
