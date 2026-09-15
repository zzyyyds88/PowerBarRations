package pricing_setting

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// PBR 单价表（design-v1 §16.9#7）。
//
// 只用于**成本折算展示**：日志里的 estimated_cost 按这张表算出来，既不参与准入，
// 也不扣任何额度（G7"只看不扣"）。单位是**人民币 / 百万 token**；某个模型不在表里
// 就表示不折算（estimated_cost = 0），而不是回退到基座的比例价。
//
// 为什么单独建表而不是复用 setting/ratio_setting：基座的 ModelRatio/CompletionRatio
// 是"以美元基准的倍率"，无法无损表达"人民币 + 输入/输出/缓存读/缓存写四个独立单价"，
// 直接改它还会污染基座默认值（适配器仍引用那些字段，属 W7 惰性遗留）。
//
// 存储走 options 表的 PBRModelPrices 键，因此随 GET/PUT /api/v1/system/options 读写，
// 也自动进 export / import（design-v1 §12.4 的幂等口径不受影响）。

// OptionKeyModelPrices 单价表在 options 表里的键。
const OptionKeyModelPrices = "PBRModelPrices"

// ModelPrice 单个模型的单价（人民币 / 百万 token）。四个字段都可留空（=0 表示不折算该口径）。
type ModelPrice struct {
	Model      string  `json:"model"`
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

var (
	mu     sync.RWMutex
	prices = map[string]ModelPrice{}
)

// FromString 从 options 值装载单价表（启动/更新时调用）。非法 JSON 返回错误并清空表。
func FromString(raw string) error {
	parsed, err := Parse(raw)
	if err != nil {
		mu.Lock()
		prices = map[string]ModelPrice{}
		mu.Unlock()
		return err
	}
	mu.Lock()
	prices = parsed
	mu.Unlock()
	return nil
}

// Parse 解析单价表 JSON 并做规范化（去空行、去重、按模型名排序）。
//
// 去重规则是"同一模型只保留最后一条"：与 PUT 覆盖语义一致，避免调用方因为
// 数组里重复了一条而拿到不确定的顺序。
func Parse(raw string) (map[string]ModelPrice, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]ModelPrice{}, nil
	}
	var list []ModelPrice
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	out := make(map[string]ModelPrice, len(list))
	for _, item := range list {
		model := strings.TrimSpace(item.Model)
		if model == "" {
			continue
		}
		item.Model = model
		if item.Input < 0 || item.Output < 0 || item.CacheRead < 0 || item.CacheWrite < 0 {
			return nil, errNegativePrice
		}
		out[model] = item
	}
	return out, nil
}

// ToString 序列化为稳定形态（按模型名排序），供落库与 export 使用。
func ToString() string {
	list := List()
	encoded, err := json.Marshal(list)
	if err != nil || len(list) == 0 {
		return "[]"
	}
	return string(encoded)
}

// List 当前单价表（按模型名升序）。
func List() []ModelPrice {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]ModelPrice, 0, len(prices))
	for _, item := range prices {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}

// Normalize 校验并规范化一份单价表（供 API 写入前调用）。
//
// 返回规范化后的列表与错误；模型名为空、重复、或出现负单价都算调用方错误。
func Normalize(list []ModelPrice) ([]ModelPrice, error) {
	seen := map[string]bool{}
	out := make([]ModelPrice, 0, len(list))
	for _, item := range list {
		item.Model = strings.TrimSpace(item.Model)
		if item.Model == "" {
			return nil, errEmptyModel
		}
		if seen[item.Model] {
			return nil, errDuplicateModel
		}
		seen[item.Model] = true
		if item.Input < 0 || item.Output < 0 || item.CacheRead < 0 || item.CacheWrite < 0 {
			return nil, errNegativePrice
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out, nil
}

// Estimate 按单价表折算一次请求的成本（人民币）。模型不在表里 → 0（不折算）。
func Estimate(model string, prompt, completion, cacheRead, cacheWrite int) float64 {
	mu.RLock()
	price, ok := prices[strings.TrimSpace(model)]
	mu.RUnlock()
	if !ok {
		return 0
	}
	const perMillion = 1_000_000.0
	cost := float64(prompt) * price.Input / perMillion
	cost += float64(completion) * price.Output / perMillion
	cost += float64(cacheRead) * price.CacheRead / perMillion
	cost += float64(cacheWrite) * price.CacheWrite / perMillion
	return cost
}

// HasModel 该模型是否配置了单价（配置了才折算）。
func HasModel(model string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := prices[strings.TrimSpace(model)]
	return ok
}

type pricingError string

func (e pricingError) Error() string { return string(e) }

const (
	errEmptyModel     pricingError = "model_prices: model name must not be empty"
	errDuplicateModel pricingError = "model_prices: duplicate model name"
	errNegativePrice  pricingError = "model_prices: price must be >= 0"
)
