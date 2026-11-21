package controller

import (
	"testing"

	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 模型测试的 max_tokens 必须给推理模型留出推理 + 输出空间：
// DeepSeek V4.1 flash 这类带 reasoning_content 的模型在 16 个 token 内无法完成
// 推理，上游直接回 502，把健康渠道测成失败
// （2026-11-21 生产 tokenav 渠道实证：16→502，64 起 200）。
func TestBuildTestRequestLeavesRoomForReasoning(t *testing.T) {
	cases := []struct {
		model string
		note  string
	}{
		{"deepseek-v4.1-flash-1017", "deepseek 推理模型（生产实证踩坑的模型名）"},
		{"gpt-4o-mini", "普通模型同样走 512 默认分支"},
		{"deepseek-r1-thinking", "thinking 字面分支"},
	}
	for _, tc := range cases {
		req := buildTestRequest(tc.model, "", nil, false)
		got, ok := req.(*dto.GeneralOpenAIRequest)
		require.True(t, ok, "chat 模型应返回 GeneralOpenAIRequest: %s", tc.model)
		assert.NotNil(t, got.MaxTokens, "MaxTokens 必须显式设置: %s (%s)", tc.model, tc.note)
		if got.MaxTokens != nil {
			assert.GreaterOrEqual(t, *got.MaxTokens, uint(512), "推理空间不足: %s (%s)", tc.model, tc.note)
		}
	}

	// o 系模型用 MaxCompletionTokens（API 不接受 max_tokens），同样给足推理空间。
	oReq := buildTestRequest("o4-mini", "", nil, false)
	got, ok := oReq.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.NotNil(t, got.MaxCompletionTokens)
	assert.GreaterOrEqual(t, *got.MaxCompletionTokens, uint(512))

	// gemini 维持既有 3000，claude thinking 保持不设语义不变。
	gReq := buildTestRequest("gemini-2.5-flash", "", nil, false)
	gotG, ok := gReq.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.NotNil(t, gotG.MaxTokens)
	assert.EqualValues(t, 3000, *gotG.MaxTokens)
}

// 前端测试对话框手动选择端点类型时走 buildTestRequest 的 endpoint_type 分支：
// 该形态的 OpenAI/Anthropic 同样不得用 16——推理模型在 16 个 token 内连推理
// 都放不下，上游直接 502（同 TestBuildTestRequestLeavesRoomForReasoning 的实证）。
func TestBuildTestRequestEndpointTypeLeavesRoomForReasoning(t *testing.T) {
	req := buildTestRequest("deepseek-v4.1-flash-1017", string(constant.EndpointTypeOpenAI), nil, false)
	got, ok := req.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.NotNil(t, got.MaxTokens)
	assert.GreaterOrEqual(t, *got.MaxTokens, uint(512), "endpoint_type=openai 分支同样必须给推理留空间")

	cReq := buildTestRequest("claude-sonnet-4-5-thinking", string(constant.EndpointTypeAnthropic), nil, false)
	gotC, ok := cReq.(*dto.ClaudeRequest)
	require.True(t, ok)
	require.NotNil(t, gotC.MaxTokens)
	assert.GreaterOrEqual(t, *gotC.MaxTokens, uint(512), "endpoint_type=anthropic 分支同样必须给推理留空间")
}
