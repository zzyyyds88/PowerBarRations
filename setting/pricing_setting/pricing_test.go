package pricing_setting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 渠道级上游单价只做成本折算展示（design-v1 §16.9#7）：这个用例锁住
// "人民币/百万 token" 的换算口径，避免以后有人把它接回扣费。
// 全局默认单价表已移除：单价只来自渠道（pbr_prices），未配价即不折算。

func TestEstimateWithPriceUsesPerMillionPricing(t *testing.T) {
	price := ModelPrice{Model: "m1", Input: 2.5, Output: 10, CacheRead: 1.25, CacheWrite: 3}

	// OpenAI/Gemini 语义：prompt 含缓存命中——计费输入 = prompt - cache_read。
	// 1M prompt（其中 1M 缓存读）→ 全价输入 0 + 输出 10 + 缓存读 1.25 + 缓存写 3。
	assert.InDelta(t, 14.25, EstimateWithPrice(price, 1_000_000, 1_000_000, 1_000_000, 1_000_000, "openai"), 1e-9)
	// 混合：700k prompt 其中 600k 缓存读 → 全价 100k×2.5 + 输出 0 + 缓存 600k×1.25。
	assert.InDelta(t, 1.0, EstimateWithPrice(price, 700_000, 0, 600_000, 0, ""), 1e-9)
	// Anthropic 语义：input 不含缓存——prompt 全额计输入价，缓存另计。
	// 1M prompt + 1M 缓存读 = 2.5 + 1.25；全缓存口径 = 2.5 + 10 + 1.25 + 3 = 16.75。
	assert.InDelta(t, 16.75, EstimateWithPrice(price, 1_000_000, 1_000_000, 1_000_000, 1_000_000, "anthropic"), 1e-9)
	// 按比例：100 万 token 单价就是数字本身，所以 1000 token = 千分之一
	assert.InDelta(t, 0.0025, EstimateWithPrice(price, 1000, 0, 0, 0, "openai"), 1e-9)
	// 扣减下限：cache_read 不会把计费输入扣成负数。
	assert.InDelta(t, 1.25, EstimateWithPrice(price, 500_000, 0, 1_000_000, 0, "openai"), 1e-9)
	// 零价与空单价都折算为 0（不折算）
	assert.Equal(t, 0.0, EstimateWithPrice(ModelPrice{Model: "m2"}, 1_000_000, 1_000_000, 1_000_000, 1_000_000, "openai"))
}

// 峰谷（design-v1 §16.9#7）：按请求开始时间匹配第一条命中的 band，未命中用
// 行级兜底价。时间一律在价格行的 timezone 下解释。

func priceFloat(value float64) *float64 { return &value }

// priceTime 构造指定时区下的本地时刻（测试可读性优先，避免手算 UTC 偏移）。
func priceTime(t *testing.T, timezone, value string) time.Time {
	t.Helper()
	location, err := time.LoadLocation(timezone)
	require.NoError(t, err)
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, location)
	require.NoError(t, err)
	return parsed
}

func allWeekdays() []int { return []int{1, 2, 3, 4, 5, 6, 7} }

func TestBandAtMatchesWeekdayWindowLeftClosedRightOpen(t *testing.T) {
	// 2026-11-02 是周一，2026-11-07 是周六，2026-11-08 是周日。
	price := ModelPrice{
		Model: "m",
		Input: 1,
		Bands: []PriceBand{{
			Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00", Input: priceFloat(5),
		}},
	}

	assert.Equal(t, 0, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-02 09:00")), "start 含")
	assert.Equal(t, 0, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-02 10:00")))
	assert.Equal(t, 0, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-02 17:59")))
	assert.Equal(t, FallbackPriceBand, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-02 18:00")), "end 不含")
	assert.Equal(t, FallbackPriceBand, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-02 08:59")))
	assert.Equal(t, FallbackPriceBand, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-07 10:00")), "周六不在 days")
	assert.Equal(t, FallbackPriceBand, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-08 10:00")), "周日不在 days")
}

func TestBandAtCrossesMidnight(t *testing.T) {
	// 周一 22:00 → 周二 02:00：时段属于**起始日**（周一）。
	price := ModelPrice{
		Bands: []PriceBand{{Days: []int{1}, Start: "22:00", End: "02:00", Input: priceFloat(7)}},
	}

	assert.Equal(t, FallbackPriceBand, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-02 21:59")), "start 之前不命中")
	assert.Equal(t, 0, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-02 22:00")))
	assert.Equal(t, 0, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-02 23:59")))
	assert.Equal(t, 0, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-03 00:00")), "延续到次日凌晨")
	assert.Equal(t, 0, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-03 01:59")))
	assert.Equal(t, FallbackPriceBand, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-03 02:00")), "end 不含")
	assert.Equal(t, FallbackPriceBand, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-03 23:00")), "周二不是 days")
	assert.Equal(t, FallbackPriceBand, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-01 23:00")), "周日不是 days")
}

func TestBandAtCrossesMidnightAcrossWeekBoundary(t *testing.T) {
	// 周日（7）22:00 → 周一 02:00：次日的命中要回看前一天=周日。
	price := ModelPrice{
		Bands: []PriceBand{{Days: []int{7}, Start: "22:00", End: "02:00", Input: priceFloat(3)}},
	}
	assert.Equal(t, 0, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-08 23:00")), "周日 23:00")
	assert.Equal(t, 0, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-09 00:30")), "周一 00:30 仍属于周日的时段")
	assert.Equal(t, FallbackPriceBand, price.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-08 12:00")))
}

func TestBandAtFirstOverlappingBandWins(t *testing.T) {
	price := ModelPrice{
		Input: 1,
		Bands: []PriceBand{
			{Days: allWeekdays(), Start: "08:00", End: "20:00", Input: priceFloat(5)},
			{Days: allWeekdays(), Start: "10:00", End: "12:00", Input: priceFloat(9)},
		},
	}
	at := priceTime(t, "Asia/Shanghai", "2026-11-02 11:00")

	assert.Equal(t, 0, price.BandAt(at), "重叠时先命中者生效")
	effective, band := price.PriceAt(at)
	assert.Equal(t, 0, band)
	assert.InDelta(t, 5, effective.Input, 1e-9, "命中第 0 条而不是更窄的第 1 条")
}

func TestPriceAtFallsBackToRowLevelAndInheritsUnsetFields(t *testing.T) {
	price := ModelPrice{
		Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4,
		Bands: []PriceBand{{
			Days: allWeekdays(), Start: "09:00", End: "18:00",
			Output: priceFloat(20), // 只覆盖输出价，其余继承行级
		}},
	}

	effective, band := price.PriceAt(priceTime(t, "Asia/Shanghai", "2026-11-02 10:00"))
	assert.Equal(t, 0, band)
	assert.InDelta(t, 1, effective.Input, 1e-9, "未填 → 行级兜底")
	assert.InDelta(t, 20, effective.Output, 1e-9, "band 覆盖")
	assert.InDelta(t, 3, effective.CacheRead, 1e-9)
	assert.InDelta(t, 4, effective.CacheWrite, 1e-9)

	// 未命中：原样返回行级价与 -1（旧单层价语义）。
	effective, band = price.PriceAt(priceTime(t, "Asia/Shanghai", "2026-11-02 20:00"))
	assert.Equal(t, FallbackPriceBand, band)
	assert.InDelta(t, 2, effective.Output, 1e-9)
}

func TestPriceAtHonorsExplicitZeroOverride(t *testing.T) {
	// 指针区分"未填"和"显式 0"：band 写 0 必须真的按 0 计，而不是继承行级价。
	price := ModelPrice{
		Input: 100,
		Bands: []PriceBand{{Days: allWeekdays(), Start: "00:00", End: "23:59", Input: priceFloat(0)}},
	}
	effective, band := price.PriceAt(priceTime(t, "Asia/Shanghai", "2026-11-02 10:00"))
	assert.Equal(t, 0, band)
	assert.InDelta(t, 0, effective.Input, 1e-9)
}

func TestBandAtUsesPriceRowTimezone(t *testing.T) {
	// 2026-11-02T02:00Z = 北京时间 10:00，同一时刻在 UTC 是 02:00。
	instant := time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)

	shanghai := ModelPrice{
		Timezone: "Asia/Shanghai",
		Bands:    []PriceBand{{Days: allWeekdays(), Start: "09:00", End: "18:00", Input: priceFloat(5)}},
	}
	assert.Equal(t, 0, shanghai.BandAt(instant), "按价格行 timezone 解释")

	utc := ModelPrice{
		Timezone: "UTC",
		Bands:    []PriceBand{{Days: allWeekdays(), Start: "09:00", End: "18:00", Input: priceFloat(5)}},
	}
	assert.Equal(t, FallbackPriceBand, utc.BandAt(instant), "同一时刻在 UTC 是 02:00")

	// 空 timezone 默认 Asia/Shanghai（design-v1 §16.9#7）。
	defaulted := ModelPrice{
		Bands: []PriceBand{{Days: allWeekdays(), Start: "09:00", End: "18:00", Input: priceFloat(5)}},
	}
	assert.Equal(t, 0, defaulted.BandAt(instant))
}

func TestBandAtIgnoresInvalidStoredBandData(t *testing.T) {
	// 写路径会拒绝这些值；历史脏数据/手改 JSON 不能 panic，只能"不命中"。
	invalidHHMM := ModelPrice{
		Bands: []PriceBand{{Days: []int{1}, Start: "9:00", End: "18:00", Input: priceFloat(5)}},
	}
	assert.Equal(t, FallbackPriceBand, invalidHHMM.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-02 10:00")))

	invalidTimezone := ModelPrice{
		Timezone: "Mars/Olympus",
		Bands:    []PriceBand{{Days: allWeekdays(), Start: "09:00", End: "18:00", Input: priceFloat(5)}},
	}
	assert.Equal(t, FallbackPriceBand, invalidTimezone.BandAt(time.Now()))

	emptyRange := ModelPrice{
		Bands: []PriceBand{{Days: []int{1}, Start: "10:00", End: "10:00", Input: priceFloat(5)}},
	}
	assert.Equal(t, FallbackPriceBand, emptyRange.BandAt(priceTime(t, "Asia/Shanghai", "2026-11-02 10:00")))
}

func TestEstimateWithPriceAtReturnsMatchedBand(t *testing.T) {
	price := ModelPrice{
		Input: 1, Output: 2,
		Bands: []PriceBand{{Days: allWeekdays(), Start: "09:00", End: "18:00", Input: priceFloat(5)}},
	}

	// 1M prompt，OpenAI 语义（无缓存）→ 命中价 5 元。
	cost, band := EstimateWithPriceAt(price, priceTime(t, "Asia/Shanghai", "2026-11-02 10:00"),
		1_000_000, 0, 0, 0, "openai")
	assert.Equal(t, 0, band)
	assert.InDelta(t, 5, cost, 1e-9)

	// 未命中 → 行级输入价 1 元 + 输出 2 元 × 1M = 3 元，band=-1。
	cost, band = EstimateWithPriceAt(price, priceTime(t, "Asia/Shanghai", "2026-11-02 20:00"),
		1_000_000, 1_000_000, 0, 0, "openai")
	assert.Equal(t, FallbackPriceBand, band)
	assert.InDelta(t, 3, cost, 1e-9)

	// 无 bands 的旧单层价：恒 -1，成本口径不变。
	cost, band = EstimateWithPriceAt(ModelPrice{Input: 1}, time.Now(), 1_000_000, 0, 0, 0, "openai")
	assert.Equal(t, FallbackPriceBand, band)
	assert.InDelta(t, 1, cost, 1e-9)
}
