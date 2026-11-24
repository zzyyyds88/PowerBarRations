package operation_setting

import (
	"strings"
	"sync"
)

var SelfUseModeEnabled = false

// 自动禁用/欠费关键词（design-v1 §7.6）：条目属部署数据，默认值只覆盖
// 各厂商的完整报文。**禁止使用 "Permission denied" / "Operation not allowed"
// 这类通用短语**——它们是子串匹配，会命中健康请求的普通错误体，把渠道误禁用。
// 运维需要更宽松的匹配时，在 PUT /api/v1/system/options 里自行添加完整报文。
var (
	automaticDisableMu       sync.RWMutex
	automaticDisableKeywords = []string{
		"Your credit balance is too low",
		"This organization has been disabled.",
		"You exceeded your current quota",
		"The security token included in the request is invalid",
		"Your account is not authorized",
	}
)

// AutomaticDisableKeywordsSnapshot 返回关键词表的只读副本。
// 调用方（路由分类、自动禁用判定）可能在请求路径上，必须用快照避免与
// 配置热更新并发读写同一个切片。
func AutomaticDisableKeywordsSnapshot() []string {
	automaticDisableMu.RLock()
	defer automaticDisableMu.RUnlock()
	out := make([]string, len(automaticDisableKeywords))
	copy(out, automaticDisableKeywords)
	return out
}

func AutomaticDisableKeywordsToString() string {
	automaticDisableMu.RLock()
	defer automaticDisableMu.RUnlock()
	return strings.Join(automaticDisableKeywords, "\n")
}

func AutomaticDisableKeywordsFromString(s string) {
	parsed := []string{}
	for k := range strings.SplitSeq(s, "\n") {
		k = strings.ToLower(strings.TrimSpace(k))
		if k != "" {
			parsed = append(parsed, k)
		}
	}
	automaticDisableMu.Lock()
	automaticDisableKeywords = parsed
	automaticDisableMu.Unlock()
}
