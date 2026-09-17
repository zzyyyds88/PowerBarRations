package model

import (
	"testing"

	"pbr/constant"
)

// TestNormalizeChannelBaseURL 覆盖 ui-spec §6.4 / api-spec §4.1 的"API 地址只填到
// /v1 即可"：协议型渠道去掉结尾版本段，其它类型原样保留。
func TestNormalizeChannelBaseURL(t *testing.T) {
	cases := []struct {
		name        string
		channelType int
		raw         string
		want        string
	}{
		{"openai without version", constant.ChannelTypeOpenAI, "https://api.openai.com", "https://api.openai.com"},
		{"openai with v1", constant.ChannelTypeOpenAI, "https://host/v1", "https://host"},
		{"openai with v1 and trailing slash", constant.ChannelTypeOpenAI, "https://host/v1/", "https://host"},
		{"openai with path prefix", constant.ChannelTypeOpenAI, "https://host/openai/v1", "https://host/openai"},
		{"openai unrelated path", constant.ChannelTypeOpenAI, "https://host/api", "https://host/api"},
		// 用户直接把完整端点粘进 base_url 是常态（第三方中转只给这一个地址）。
		{"openai full chat endpoint", constant.ChannelTypeOpenAI, "https://host/v1/chat/completions", "https://host"},
		{"openai chat endpoint no version", constant.ChannelTypeOpenAI, "https://host/chat/completions", "https://host"},
		{"openai full responses endpoint", constant.ChannelTypeOpenAI, "https://host/v1/responses", "https://host"},
		{"openai responses compact endpoint", constant.ChannelTypeOpenAI, "https://host/v1/responses/compact", "https://host"},
		{"openai prefix plus full endpoint", constant.ChannelTypeOpenAI, "https://host/openai/v1/chat/completions", "https://host/openai"},
		{"anthropic full messages endpoint", constant.ChannelTypeAnthropic, "https://host/v1/messages", "https://host"},
		{"anthropic with v1", constant.ChannelTypeAnthropic, "https://api.anthropic.com/v1", "https://api.anthropic.com"},
		{"gemini with v1beta", constant.ChannelTypeGemini, "https://generativelanguage.googleapis.com/v1beta", "https://generativelanguage.googleapis.com"},
		{"gemini with v1", constant.ChannelTypeGemini, "https://host/v1", "https://host"},
		{"gemini with v1alpha", constant.ChannelTypeGemini, "https://host/v1alpha", "https://host"},
		// 非协议型：不动。Custom 有自己的 {model}/完整端点补全规则；Azure 路径结构不同。
		{"custom unchanged", constant.ChannelTypeCustom, "https://host/v1", "https://host/v1"},
		{"azure unchanged", constant.ChannelTypeAzure, "https://host/v1", "https://host/v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeChannelBaseURL(tc.channelType, tc.raw); got != tc.want {
				t.Fatalf("normalizeChannelBaseURL(%d, %q) = %q, want %q", tc.channelType, tc.raw, got, tc.want)
			}
		})
	}
}

// TestDeriveOpenAICompatibleModelsURL 覆盖 Custom(8) 未配置 /v1/models 路由时的
// 兜底探测地址推导（ui-spec §6.4）。
func TestDeriveOpenAICompatibleModelsURL(t *testing.T) {
	cases := []struct{ name, raw, want string }{
		{"full chat endpoint", "https://host/v1/chat/completions", "https://host/v1/models"},
		{"bare host", "https://host", "https://host/v1/models"},
		{"version segment", "https://host/v1", "https://host/v1/models"},
		{"prefix plus endpoint", "https://host/compatible-mode/v1/chat/completions", "https://host/compatible-mode/v1/models"},
		{"gemini version", "https://host/v1beta", "https://host/v1beta/models"},
		{"trailing slash", "https://host/v1/chat/completions/", "https://host/v1/models"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveOpenAICompatibleModelsURL(tc.raw); got != tc.want {
				t.Fatalf("DeriveOpenAICompatibleModelsURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestGetBaseURLNormalizesVersionSegment 确认 GetBaseURL 走的是归一化后的值，
// 且空 base_url 仍回落到内置地址。
func TestGetBaseURLNormalizesVersionSegment(t *testing.T) {
	withV1 := "https://host/v1"
	ch := &Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &withV1}
	if got := ch.GetBaseURL(); got != "https://host" {
		t.Fatalf("GetBaseURL() = %q, want %q", got, "https://host")
	}

	empty := ""
	fallback := &Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &empty}
	if got, want := fallback.GetBaseURL(), constant.GetChannelBaseURL(constant.ChannelTypeOpenAI); got != want {
		t.Fatalf("GetBaseURL() fallback = %q, want %q", got, want)
	}

	var nilBase *string
	nilChannel := &Channel{Type: constant.ChannelTypeOpenAI, BaseURL: nilBase}
	if got := nilChannel.GetBaseURL(); got != "" {
		t.Fatalf("GetBaseURL() with nil base = %q, want empty", got)
	}
}
