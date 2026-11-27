package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 自动健康检查的失败/禁用判定：慢响应/本地构造失败只计失败；凭据/额度
// 故障即使同时带 localErr（testChannel 的真实失败形状）也必须禁用。
// 修改进程级 AutomaticDisableChannelEnabled，测试之间必须串行；
// 禁止对本文件用例加 t.Parallel。
func TestEvaluateChannelTestSeparatesSlowAndLocalFailuresFromDisable(t *testing.T) {
	previous := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticDisableChannelEnabled = previous })

	t.Run("localErr only counts as failure", func(t *testing.T) {
		err, ban := evaluateChannelTest(testResult{localErr: errors.New("unsupported channel type")}, 10, 1000)
		assert.Nil(t, err)
		assert.False(t, ban)
	})

	t.Run("slow success is a failure but not a ban", func(t *testing.T) {
		err, ban := evaluateChannelTest(testResult{}, 5000, 1000)
		require.NotNil(t, err)
		assert.Equal(t, types.ErrorCodeChannelResponseTimeExceeded, err.GetErrorCode())
		assert.False(t, ban)
	})

	t.Run("timeout is not a ban", func(t *testing.T) {
		timeoutErr := types.NewOpenAIError(
			errors.New("upstream timeout: "+context.DeadlineExceeded.Error()),
			types.ErrorCode(""), http.StatusGatewayTimeout,
		)
		_, ban := evaluateChannelTest(testResult{newAPIError: timeoutErr}, 10, 1000)
		assert.False(t, ban)
	})

	t.Run("credential failure still bans", func(t *testing.T) {
		authErr := types.NewOpenAIError(errors.New("invalid api key"), types.ErrorCode(""), http.StatusUnauthorized)
		_, ban := evaluateChannelTest(testResult{newAPIError: authErr}, 10, 1000)
		assert.True(t, ban)
	})

	// testChannel 的真实失败路径同时设置 localErr 与 newAPIError（尤其
	// SetupContext 取不到 key）；凭据故障不能因 localErr 非空而漏掉禁用。
	t.Run("credential failure with localErr still bans", func(t *testing.T) {
		authErr := types.NewOpenAIError(errors.New("no available key"), types.ErrorCodeChannelNoAvailableKey, http.StatusInternalServerError)
		_, ban := evaluateChannelTest(testResult{localErr: authErr, newAPIError: authErr}, 10, 1000)
		assert.True(t, ban)
	})

	// 本地构造失败（不支持的类型/无效 api type）是 5xx，不属于禁用白名单。
	t.Run("local construction failure does not ban", func(t *testing.T) {
		localErr := errors.New("unsupported channel type")
		apiErr := types.NewError(localErr, types.ErrorCodeInvalidApiType)
		_, ban := evaluateChannelTest(testResult{localErr: localErr, newAPIError: apiErr}, 10, 1000)
		assert.False(t, ban)
	})
}

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
