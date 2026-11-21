package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	relayconstant "github.com/zzyyyds88/PowerBarRations/relay/constant"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// 流内错误帧（routing-spec §4.4）：OpenAI chat / Responses 两条入站流式路径
// 都必须做到"提交前不转发并换成员、提交后透传并留痕、心跳与 error:null 不误判"。

func init() {
	gin.SetMode(gin.TestMode)
	// 测试进程没有加载运行配置：显式给流式超时，避免 NewTicker(0) panic。
	if constant.StreamingTimeout == 0 {
		constant.StreamingTimeout = 30
	}
}

func newStreamRejectContext(t *testing.T, body string, format types.RelayFormat) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RelayFormat:     format,
		OriginModelName: "lane-model",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "lane-model"},
		DisablePing:     true,
	}
	return c, recorder, resp, info
}

func assertUpstreamReject(t *testing.T, apiErr *types.NewAPIError, recorder *httptest.ResponseRecorder) {
	t.Helper()
	require.NotNil(t, apiErr, "提交前的流内错误帧必须让本次尝试失败")
	assert.Equal(t, types.ErrorCodeUpstreamReject, apiErr.GetErrorCode())
	assert.Empty(t, recorder.Body.String(), "提交前不得把错误帧写给客户端")
}

func TestOaiStreamHandlerRejectsErrorFrameBeforeCommit(t *testing.T) {
	body := "data: {\"error\":{\"message\":\"Output data may contain inappropriate content.\",\"type\":\"invalid_request_error\"}}\n" +
		"data: [DONE]\n"
	c, recorder, resp, info := newStreamRejectContext(t, body, types.RelayFormatOpenAI)

	_, apiErr := OaiStreamHandler(c, info, resp)

	assertUpstreamReject(t, apiErr, recorder)
	assert.Contains(t, apiErr.Error(), "inappropriate content", "msg 取帧内上游报文")
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject), "提交前不走提交后留痕")
}

func TestOaiStreamHandlerPassesThroughErrorFrameAfterCommit(t *testing.T) {
	body := "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"lane-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n" +
		"data: {\"error\":{\"message\":\"content blocked\"}}\n" +
		"data: [DONE]\n"
	c, recorder, resp, info := newStreamRejectContext(t, body, types.RelayFormatOpenAI)

	_, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, apiErr, "提交后不换成员、不报错")
	assert.Contains(t, recorder.Body.String(), "hello")
	assert.Contains(t, recorder.Body.String(), "content blocked", "提交后错误帧原样透传")
	assert.Equal(t, "content blocked", common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject),
		"提交后如实留痕 error_kind=upstream_reject 的报文")
}

func TestOaiStreamHandlerIgnoresHeartbeatAndNullError(t *testing.T) {
	// "error":null 不是错误帧（§4.4 判定只认非空 error 对象）；携带内容的帧
	// 照常提交透传。零载荷帧（delta:{}）按内容帧口径是开场帧，见空流用例。
	body := "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"lane-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}],\"error\":null}\n" +
		"data: [DONE]\n"
	c, recorder, resp, info := newStreamRejectContext(t, body, types.RelayFormatOpenAI)

	_, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, apiErr, "error:null 不是错误帧")
	assert.Contains(t, recorder.Body.String(), "\"error\":null", "心跳/正常帧照常转发")
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}

// 零载荷帧（delta:{} + "error":null）没有任何内容：按内容帧口径是空流，
// 不是成功——即使它不是错误帧（§4.4 判定与空流判定是两回事）。
func TestOaiStreamHandlerZeroPayloadFrameIsEmptyStream(t *testing.T) {
	body := "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"lane-model\",\"choices\":[{\"index\":0,\"delta\":{}}],\"error\":null}\n" +
		"data: [DONE]\n"
	c, recorder, resp, info := newStreamRejectContext(t, body, types.RelayFormatOpenAI)

	_, apiErr := OaiStreamHandler(c, info, resp)

	require.NotNil(t, apiErr, "零载荷帧 = 空流，必须换成员")
	assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject),
		"不是流内错误帧：失败原因是空响应而非上游拒绝")
	assert.Empty(t, recorder.Body.String(), "提交前不写任何帧给客户端")
}

func TestOaiResponsesStreamHandlerRejectsFailedFrameBeforeCommit(t *testing.T) {
	// 提交口径是"第一个写给客户的 data 帧"（§3 第 6 步）：错误帧必须排在
	// response.created 之类的生命周期帧之前，才是真正的"提交前"。
	body := "event: response.failed\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"r1\",\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"model overloaded\"}}}\n"
	c, recorder, resp, info := newStreamRejectContext(t, body, types.RelayFormatOpenAIResponses)

	_, apiErr := OaiResponsesStreamHandler(c, info, resp)

	assertUpstreamReject(t, apiErr, recorder)
	assert.Contains(t, apiErr.Error(), "model overloaded")
}

func TestOaiResponsesStreamHandlerPassesThroughFailedFrameAfterCommit(t *testing.T) {
	body := "event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n" +
		"event: response.failed\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"r1\",\"status\":\"failed\",\"error\":{\"message\":\"model overloaded\"}}}\n"
	c, recorder, resp, info := newStreamRejectContext(t, body, types.RelayFormatOpenAIResponses)

	_, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, apiErr, "提交后不换成员、不报错")
	assert.Contains(t, recorder.Body.String(), "hi")
	assert.Contains(t, recorder.Body.String(), "response.failed", "提交后错误帧原样透传")
	assert.Equal(t, "model overloaded", common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}

func TestOaiResponsesStreamHandlerIgnoresFailedWithoutError(t *testing.T) {
	// 已提交（内容帧已交付）后的 response.failed 不带 error 对象：不是流内
	// 错误帧（§4.4 只认非空 error 对象），原样透传、不留痕。
	body := "event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"r1\",\"status\":\"failed\"}}\n"
	c, recorder, resp, info := newStreamRejectContext(t, body, types.RelayFormatOpenAIResponses)

	_, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, apiErr, "不带 error 的 response.failed 不是错误帧")
	assert.Contains(t, recorder.Body.String(), "response.failed")
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}

// 零载荷：仅 response.failed（不带 error、无任何内容帧）= 空流，换成员。
func TestOaiResponsesStreamHandlerFailedWithoutErrorAloneIsEmptyStream(t *testing.T) {
	body := "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"r1\",\"status\":\"failed\"}}\n"
	c, _, resp, info := newStreamRejectContext(t, body, types.RelayFormatOpenAIResponses)

	_, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.NotNil(t, apiErr, "零载荷 = 空流")
	assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}

func TestOaiResponsesToChatStreamHandlerRejectsErrorFrameBeforeCommit(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{\"type\":\"server_error\",\"message\":\"upstream exploded\"}}\n"
	c, recorder, resp, info := newStreamRejectContext(t, body, types.RelayFormatOpenAI)

	_, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)

	assertUpstreamReject(t, apiErr, recorder)
}

func TestOaiChatToResponsesStreamHandlerRejectsErrorFrameBeforeCommit(t *testing.T) {
	body := "data: {\"error\":{\"message\":\"content blocked\"}}\n"
	c, recorder, resp, info := newStreamRejectContext(t, body, types.RelayFormatOpenAIResponses)

	_, apiErr := OaiChatToResponsesStreamHandler(c, info, resp)

	assertUpstreamReject(t, apiErr, recorder)
}

func TestOaiChatToResponsesStreamHandlerPassesThroughAfterCommit(t *testing.T) {
	body := "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"lane-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n" +
		"data: {\"error\":{\"message\":\"content blocked\"}}\n" +
		"data: [DONE]\n"
	c, _, resp, info := newStreamRejectContext(t, body, types.RelayFormatOpenAIResponses)

	_, apiErr := OaiChatToResponsesStreamHandler(c, info, resp)

	require.Nil(t, apiErr, "提交后按 Responses 协议写失败终态事件并正常收尾")
	assert.Equal(t, "content blocked", common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}

// 上游建流后零有效帧必须判成成员级失败，不能记成功。
//
// 线上实案（日志 #230647）：200 + 首事件超时 → received=0，但 handler 把
// "末帧解析失败"只写日志就吞掉，于是落一条 success=true / completion=0，
// 客户端干等 74 秒拿到空流，车道运行态完全没记这次失败。
func TestOaiStreamHandlerEmptyStreamIsFailure(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"完全空流", ""},
		{"只有 [DONE]", "data: [DONE]\n"},
		{"只有心跳注释", ": PING\n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder, resp, info := newStreamRejectContext(t, tc.body, types.RelayFormatOpenAI)

			usage, apiErr := OaiStreamHandler(c, info, resp)

			require.NotNil(t, apiErr, "零帧空流必须让本次尝试失败")
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Nil(t, usage, "失败尝试不返回 usage")
			assert.Empty(t, recorder.Body.String(), "提交前不写任何帧给客户端")
		})
	}
}

// 正常流不能因为守卫被误判成空流。
func TestOaiStreamHandlerNormalStreamStillSucceeds(t *testing.T) {
	body := "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"lane-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n" +
		"data: [DONE]\n"
	c, recorder, resp, info := newStreamRejectContext(t, body, types.RelayFormatOpenAI)

	_, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	assert.Contains(t, recorder.Body.String(), "hi")
}

// /v1/messages 入站经 OpenAI 上游时，空流同样要判失败（而不是返回 200 空流）。
func TestOaiStreamHandlerEmptyStreamIsFailureForClaudeFormat(t *testing.T) {
	c, recorder, resp, info := newStreamRejectContext(t, "", types.RelayFormatClaude)

	_, apiErr := OaiStreamHandler(c, info, resp)

	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
	assert.Empty(t, recorder.Body.String())
}
