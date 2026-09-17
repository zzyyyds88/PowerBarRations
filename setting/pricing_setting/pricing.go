package pricing_setting

// PBR 渠道级上游单价的折算算术（design-v1 §16.9#7）。
//
// 只用于**成本折算展示**：日志里的 estimated_cost 按渠道配的单价算出来，既不参与准入，
// 也不扣任何额度（G7"只看不扣"）。单位是**人民币 / 百万 token**。
//
// 单层单价：唯一的价格来源是渠道级上游单价（channel.Setting.pbr_prices，按请求模型
// 匹配）；渠道未配价 → 不折算（estimated_cost = 0）。没有全局默认单价表——单用户
// 自用网关，每个渠道自己定价即可。本包不再持有任何全局状态，只保留纯函数。

// ModelPrice 单个模型的单价（人民币 / 百万 token）。四个字段都可留空（=0 表示不折算该口径）。
type ModelPrice struct {
	Model      string  `json:"model"`
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

// EstimateWithPrice 用指定单价折算一次请求成本（人民币）。渠道级上游单价走这里。
func EstimateWithPrice(price ModelPrice, prompt, completion, cacheRead, cacheWrite int) float64 {
	const perMillion = 1_000_000.0
	cost := float64(prompt) * price.Input / perMillion
	cost += float64(completion) * price.Output / perMillion
	cost += float64(cacheRead) * price.CacheRead / perMillion
	cost += float64(cacheWrite) * price.CacheWrite / perMillion
	return cost
}
