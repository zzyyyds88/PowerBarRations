package claude

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
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// 流内错误帧（routing-spec §4.4）：Anthropic `/v1/messages` SSE 的 `event: error`
// 与 data 内的 `type=error` 同义；提交前不转发并换成员，提交后透传并留痕。

func init() {
	gin.SetMode(gin.TestMode)
	// 测试进程没有加载运行配置：显式给流式超时，避免 NewTicker(0) panic。
	if constant.StreamingTimeout == 0 {
		constant.StreamingTimeout = 30
	}
}

func newClaudeStreamRejectContext(t *testing.T, body string, format types.RelayFormat) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	info := &relaycommon.RelayInfo{
		RelayFormat:     format,
		OriginModelName: "lane-model",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "claude-x"},
		DisablePing:     true,
	}
	return c, recorder, resp, info
}

const claudeMessageStartFrame = "event: message_start\n" +
	"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-x\",\"content\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n"

const claudeErrorMessage = "Overloaded"

const claudeErrorFrame = "event: error\n" +
	"data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"" + claudeErrorMessage + "\"}}\n"

func TestClaudeStreamHandlerRejectsErrorEventBeforeCommit(t *testing.T) {
	c, recorder, resp, info := newClaudeStreamRejectContext(t, claudeErrorFrame, types.RelayFormatClaude)

	_, apiErr := ClaudeStreamHandler(c, resp, info)

	require.NotNil(t, apiErr, "提交前的 event: error 必须让本次尝试失败")
	assert.Equal(t, types.ErrorCodeUpstreamReject, apiErr.GetErrorCode())
	assert.Empty(t, recorder.Body.String(), "提交前不得把错误事件写给客户端")
	assert.Contains(t, apiErr.Error(), claudeErrorMessage)
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}

func TestClaudeStreamHandlerRejectsErrorObjectBeforeCommit(t *testing.T) {
	// Anthropic 之外的兼容上游可能只给 error 对象、不给 type。
	body := "data: {\"error\":{\"type\":\"api_error\",\"message\":\"upstream exploded\"}}\n"
	c, recorder, resp, info := newClaudeStreamRejectContext(t, body, types.RelayFormatClaude)

	_, apiErr := ClaudeStreamHandler(c, resp, info)

	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeUpstreamReject, apiErr.GetErrorCode())
	assert.Empty(t, recorder.Body.String())
}

func TestClaudeStreamHandlerPassesThroughErrorEventAfterCommit(t *testing.T) {
	c, recorder, resp, info := newClaudeStreamRejectContext(t, claudeMessageStartFrame+claudeErrorFrame, types.RelayFormatClaude)

	_, apiErr := ClaudeStreamHandler(c, resp, info)

	require.Nil(t, apiErr, "提交后不换成员、不报错")
	written := recorder.Body.String()
	assert.Contains(t, written, "event: message_start")
	assert.Contains(t, written, "event: error", "提交后的错误事件按 Anthropic SSE 原样透传")
	assert.Contains(t, written, claudeErrorMessage)
	assert.Equal(t, claudeErrorMessage, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}

func TestClaudeStreamHandlerIgnoresNullError(t *testing.T) {
	body := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-x\",\"content\":[],\"error\":null}}\n"
	c, recorder, resp, info := newClaudeStreamRejectContext(t, body, types.RelayFormatClaude)

	_, apiErr := ClaudeStreamHandler(c, resp, info)

	require.Nil(t, apiErr, "error:null 不是错误帧")
	assert.Contains(t, recorder.Body.String(), "message_start")
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}

func TestClaudeResponsesStreamHandlerRejectsErrorEventBeforeCommit(t *testing.T) {
	c, recorder, resp, info := newClaudeStreamRejectContext(t, claudeErrorFrame, types.RelayFormatOpenAIResponses)

	_, apiErr := ClaudeResponsesStreamHandler(c, resp, info)

	require.NotNil(t, apiErr, "不能把提交前的错误帧改写成会写客户端的 response.failed")
	assert.Equal(t, types.ErrorCodeUpstreamReject, apiErr.GetErrorCode())
	assert.Empty(t, recorder.Body.String())
}

func TestClaudeResponsesStreamHandlerPassesThroughErrorEventAfterCommit(t *testing.T) {
	c, recorder, resp, info := newClaudeStreamRejectContext(t, claudeMessageStartFrame+claudeErrorFrame, types.RelayFormatOpenAIResponses)

	usage, apiErr := ClaudeResponsesStreamHandler(c, resp, info)

	require.Nil(t, apiErr, "提交后按 Responses 协议写失败终态事件并正常收尾")
	assert.NotNil(t, usage)
	assert.Contains(t, recorder.Body.String(), "response.failed", "客户端拿到协议内的失败终态")
	assert.Equal(t, claudeErrorMessage, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}
