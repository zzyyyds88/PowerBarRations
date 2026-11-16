package pricing_setting

// PBR 渠道级上游单价的折算算术（design-v1 §16.9#7）。
//
// 只用于**成本折算展示**：日志里的 estimated_cost 按渠道配的单价算出来，既不参与准入，
// 也不扣任何额度（G7"只看不扣"）。单位是**人民币 / 百万 token**。
//
// 单价：唯一的价格来源是渠道级上游单价（channel.Setting.pbr_prices，按请求模型
// 匹配）；渠道未配价 → 不折算（estimated_cost = 0）。没有全局默认单价表——单用户
// 自用网关，每个渠道自己定价即可。本包不再持有任何全局状态，只保留纯函数。
//
// 峰谷（可选）：价格行可带 timezone + 有序 bands。按**请求开始时间**在该时区匹配
// 第一条命中的 band；未命中用行级 input/output/cache_read/cache_write 兜底
// （旧单层价天然等价，无需数据迁移）。band 之间允许重叠，先命中者生效。

import (
	"strings"
	"sync"
	"time"
	// 容器镜像通常不带 tzdata：内嵌 IANA 时区库，保证 time.LoadLocation 在
	// 任何部署形态下都可用（design-v1 §16.9#7）。
	_ "time/tzdata"

	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
)

// FallbackPriceBand 未命中任何峰谷时段（使用行级兜底价）；日志 price_band
// 用同一口径（-1）。
const FallbackPriceBand = -1

// PriceBand 峰谷时段（与 dto.PriceBand 同形；本包不依赖 HTTP/JSON 契约层语义）。
//
// Days 为 1–7（周一=1）；Start/End 为 HH:MM，左闭右开；Start > End 表示跨午夜
// （时段从 Days 列出的那一天 Start 开始，延续到次日 End）。价格字段为 nil 表示
// 未填，继承行级兜底价。
type PriceBand struct {
	Days       []int    `json:"days"`
	Start      string   `json:"start"`
	End        string   `json:"end"`
	Input      *float64 `json:"input,omitempty"`
	Output     *float64 `json:"output,omitempty"`
	CacheRead  *float64 `json:"cache_read,omitempty"`
	CacheWrite *float64 `json:"cache_write,omitempty"`
}

// ModelPrice 单个模型的单价（人民币 / 百万 token）。四个价格字段都可留空（=0 表示不折算该口径）。
type ModelPrice struct {
	Model      string  `json:"model"`
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
	// Timezone 峰谷时段所在时区（IANA 名）；空值按 dto.DefaultPriceTimezone。
	Timezone string `json:"timezone,omitempty"`
	// Bands 有序峰谷时段；空 = 全天使用行级兜底价。
	Bands []PriceBand `json:"bands,omitempty"`
}

// PriceTimezone 返回规范化后的时区名；空值回落 Asia/Shanghai。
func (p ModelPrice) PriceTimezone() string {
	if tz := strings.TrimSpace(p.Timezone); tz != "" {
		return tz
	}
	return dto.DefaultPriceTimezone
}

// priceLocations 缓存已解析的时区。取值来自部署配置（渠道价格表），数量由配置
// 本身限定；无效时区缓存为 nil，避免每个请求都重复读 tzdata。
var priceLocations sync.Map // timezone name -> *time.Location

func loadPriceLocation(name string) *time.Location {
	if cached, ok := priceLocations.Load(name); ok {
		location, _ := cached.(*time.Location)
		return location
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		location = nil
	}
	priceLocations.Store(name, location)
	return location
}

// BandAt 返回 at 命中的第一条 band 下标；未命中或时区不可解析时返回
// FallbackPriceBand（-1）。at 只需是绝对时刻，时区换算在内部完成。
func (p ModelPrice) BandAt(at time.Time) int {
	if len(p.Bands) == 0 {
		return FallbackPriceBand
	}
	location := loadPriceLocation(p.PriceTimezone())
	if location == nil {
		return FallbackPriceBand
	}
	local := at.In(location)
	day := int(local.Weekday())
	if day == 0 { // time.Weekday: Sunday=0；价格契约 Monday=1..Sunday=7。
		day = 7
	}
	minutes := local.Hour()*60 + local.Minute()
	for i := range p.Bands {
		if p.Bands[i].matches(day, minutes) {
			return i
		}
	}
	return FallbackPriceBand
}

// matches 判断 Day（1–7）的 minutes（一天内分钟数）是否落在该时段内。
//
// 跨午夜语义：Start > End 的时段属于 Days 列出的那一天，从当天 Start 延续到
// 次日 End；因此次日凌晨的命中由"前一天在 Days 里"决定。
func (b PriceBand) matches(day, minutes int) bool {
	start, ok := dto.ParseClockHHMM(b.Start)
	if !ok {
		return false
	}
	end, ok := dto.ParseClockHHMM(b.End)
	if !ok {
		return false
	}
	switch {
	case start < end:
		return containsDay(b.Days, day) && minutes >= start && minutes < end
	case start > end:
		if containsDay(b.Days, day) && minutes >= start {
			return true
		}
		previousDay := day - 1
		if previousDay < 1 {
			previousDay = 7
		}
		return containsDay(b.Days, previousDay) && minutes < end
	default:
		// start == end 是空区间（写路径已拒绝）；历史脏数据按不命中处理。
		return false
	}
}

func containsDay(days []int, day int) bool {
	for _, candidate := range days {
		if candidate == day {
			return true
		}
	}
	return false
}

// PriceAt 返回 at 时刻生效的单价与 band 下标：命中 band 时用 band 里显式填写的
// 字段覆盖行级价，其余字段继承行级兜底价；未命中返回行级价与 -1。
func (p ModelPrice) PriceAt(at time.Time) (ModelPrice, int) {
	index := p.BandAt(at)
	if index < 0 {
		return p, FallbackPriceBand
	}
	effective := p
	band := p.Bands[index]
	if band.Input != nil {
		effective.Input = *band.Input
	}
	if band.Output != nil {
		effective.Output = *band.Output
	}
	if band.CacheRead != nil {
		effective.CacheRead = *band.CacheRead
	}
	if band.CacheWrite != nil {
		effective.CacheWrite = *band.CacheWrite
	}
	return effective, index
}

// EstimateWithPriceAt 在 EstimateWithPrice 之上按请求开始时间选价，并返回命中的
// band 下标（-1 = 行级兜底）。计价口径与 EstimateWithPrice 完全一致。
func EstimateWithPriceAt(price ModelPrice, at time.Time, prompt, completion, cacheRead, cacheWrite int, semantic string) (float64, int) {
	effective, band := price.PriceAt(at)
	return EstimateWithPrice(effective, prompt, completion, cacheRead, cacheWrite, semantic), band
}

// EstimateWithPrice 用指定单价折算一次请求成本（人民币）。渠道级上游单价走这里。
//
// **语义参数（2026-10-25 修复缓存重复计费）**：OpenAI/Gemini 口径的
// prompt_tokens **包含**缓存命中部分（cached_tokens 是子集）——缓存命中的 token
// 若按 prompt 全额计输入价、再按 cache_read 计缓存价，等于收两次费（实测
// 长上下文高缓存场景单条虚高约 30 倍）。因此 prompt 要先扣掉 cache_read 再按
// 输入价计。Anthropic 口径的 input_tokens **不含**缓存（cache_read/creation
// 单独列出），prompt 全额计输入价、缓存各按自己的价。
// semantic 取 dto.BillingUsageSemantic* 常量（"anthropic"/"gemini"/"openai"，
// 空串按 openai——历史明细的绝大多数是 OpenAI 兼容渠道）。
func EstimateWithPrice(price ModelPrice, prompt, completion, cacheRead, cacheWrite int, semantic string) float64 {
	const perMillion = 1_000_000.0
	billablePrompt := prompt
	if semantic != "anthropic" {
		// OpenAI / Gemini / 未知语义：缓存命中是 prompt 的子集，先扣再计输入价。
		billablePrompt = prompt - cacheRead
		if billablePrompt < 0 {
			billablePrompt = 0
		}
	}
	cost := float64(billablePrompt) * price.Input / perMillion
	cost += float64(completion) * price.Output / perMillion
	cost += float64(cacheRead) * price.CacheRead / perMillion
	cost += float64(cacheWrite) * price.CacheWrite / perMillion
	return cost
}
