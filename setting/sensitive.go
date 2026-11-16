package setting

import "strings"

// 敏感词拦截的**默认值是关闭**（2026-10-10 董事长令）：PBR 是自用网关，
// 默认不对任何请求做词表拦截——拦截要么是上游厂商的事（内容审查由它返回 4xx，
// 由车道按成员级拒绝处理），要么由使用者在系统选项里显式打开并自带词表。
//
// 打开方式（运行时开关，无需改代码）：`PUT /api/system/options/all`
// `{"key":"CheckSensitiveEnabled","value":"true"}`，并按需写入 `SensitiveWords`。
var CheckSensitiveEnabled = false
var CheckSensitiveOnPromptEnabled = true

//var CheckSensitiveOnCompletionEnabled = true

// StopOnSensitiveEnabled 如果检测到敏感词，是否立刻停止生成，否则替换敏感词
var StopOnSensitiveEnabled = true

// StreamCacheQueueLength 流模式缓存队列长度，0表示无缓存
var StreamCacheQueueLength = 0

// SensitiveWords 敏感词。
//
// **默认为空**：词表是部署方自己的数据（design-v1 §14.9 文档纪律），
// 仓库不预置任何具体词——曾预置的测试词 `test_sensitive` 会让"打开开关"
// 立刻误伤，已移除。
var SensitiveWords = []string{}

func SensitiveWordsToString() string {
	return strings.Join(SensitiveWords, "\n")
}

func SensitiveWordsFromString(s string) {
	SensitiveWords = []string{}
	sw := strings.SplitSeq(s, "\n")
	for w := range sw {
		w = strings.TrimSpace(w)
		if w != "" {
			SensitiveWords = append(SensitiveWords, w)
		}
	}
}

func ShouldCheckPromptSensitive() bool {
	return CheckSensitiveEnabled && CheckSensitiveOnPromptEnabled
}

//func ShouldCheckCompletionSensitive() bool {
//	return CheckSensitiveEnabled && CheckSensitiveOnCompletionEnabled
//}

// ChannelSystemPromptEnabled 是否允许**渠道级系统提示词注入**
// （`ChannelSettings.SystemPrompt` / `SystemPromptOverride`）。
//
// **默认关闭**（2026-10-10 董事长令）：PBR 是自用网关，中转时不应悄悄改写
// 模型看到的提示词——这类"内置提示词"是接管的基座能力，只在明确需要时开启。
// 关闭时注入路径整体短路：请求原样转发，绝不前插/拼接任何 system 内容。
//
// 打开方式（运行时，无需改代码）：`PUT /api/system/options/all`
// `{"key":"ChannelSystemPromptEnabled","value":"true"}`。
var ChannelSystemPromptEnabled = false

// ShouldInjectChannelSystemPrompt 注入路径的唯一判据（调用方只判这个，
// 不要各自读变量，否则将来改默认值会漏改）。
func ShouldInjectChannelSystemPrompt() bool {
	return ChannelSystemPromptEnabled
}
