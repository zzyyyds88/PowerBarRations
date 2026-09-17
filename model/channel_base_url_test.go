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
