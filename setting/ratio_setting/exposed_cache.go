package ratio_setting

import "sync/atomic"

// 比率公开接口（/api/ratio 及 expose_ratio 开关）已随计费执行链退役，
// 这里仅保留失效钩子：各 Update*ByJSONString 与模型定价写入路径仍会调用
// InvalidateExposedDataCache，保持既有调用方签名不变。
type exposedCache struct{}

var exposedData atomic.Value

func InvalidateExposedDataCache() {
	exposedData.Store((*exposedCache)(nil))
}
