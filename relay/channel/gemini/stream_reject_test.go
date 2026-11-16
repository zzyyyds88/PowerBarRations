package gemini

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

// 流内错误帧（routing-spec §4.4）：Gemini 流式 JSON 帧只要带非空 error 对象就是
// 上游拒绝；提交前不转发并换成员，提交后原样透传并留痕。

func init() {
	gin.SetMode(gin.TestMode)
	// 测试进程没有加载运行配置：显式给流式超时，避免 NewTicker(0) panic。
	if constant.StreamingTimeout == 0 {
		constant.StreamingTimeout = 30
	}
}

func newGeminiStreamRejectContext(t *testing.T, body string, format types.RelayFormat) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	info := &relaycommon.RelayInfo{
		RelayFormat:     format,
		OriginModelName: "lane-model",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-pro"},
		DisablePing:     true,
	}
	return c, recorder, resp, info
}

const geminiErrorMessage = "User location is not supported"

const geminiErrorFrame = "data: {\"error\":{\"code\":400,\"message\":\"" + geminiErrorMessage + "\",\"status\":\"INVALID_ARGUMENT\"}}\n"

const geminiContentFrame = "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}],\"role\":\"model\"},\"index\":0}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":1,\"totalTokenCount\":4}}\n"

func TestGeminiNativeStreamRejectsErrorFrameBeforeCommit(t *testing.T) {
	c, recorder, resp, info := newGeminiStreamRejectContext(t, geminiErrorFrame, types.RelayFormatGemini)

	_, apiErr := GeminiTextGenerationStreamHandler(c, info, resp)

	require.NotNil(t, apiErr, "提交前的 Gemini 错误帧必须让本次尝试失败")
	assert.Equal(t, types.ErrorCodeUpstreamReject, apiErr.GetErrorCode())
	assert.Empty(t, recorder.Body.String(), "提交前不得把错误帧写给客户端")
	assert.Contains(t, apiErr.Error(), geminiErrorMessage)
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}

func TestGeminiNativeStreamPassesThroughErrorFrameAfterCommit(t *testing.T) {
	c, recorder, resp, info := newGeminiStreamRejectContext(t, geminiContentFrame+geminiErrorFrame, types.RelayFormatGemini)

	_, apiErr := GeminiTextGenerationStreamHandler(c, info, resp)

	require.Nil(t, apiErr, "提交后不换成员、不报错")
	written := recorder.Body.String()
	assert.Contains(t, written, "\"hi\"")
	assert.Contains(t, written, geminiErrorMessage, "提交后错误帧按原生 Gemini 帧原样透传")
	assert.Equal(t, geminiErrorMessage, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}

func TestGeminiNativeStreamIgnoresNullError(t *testing.T) {
	body := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]}}],\"error\":null}\n"
	c, recorder, resp, info := newGeminiStreamRejectContext(t, body, types.RelayFormatGemini)

	_, apiErr := GeminiTextGenerationStreamHandler(c, info, resp)

	require.Nil(t, apiErr, "error:null 不是错误帧")
	assert.Contains(t, recorder.Body.String(), "\"hi\"")
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}

func TestGeminiChatStreamRejectsErrorFrameBeforeCommit(t *testing.T) {
	c, recorder, resp, info := newGeminiStreamRejectContext(t, geminiErrorFrame, types.RelayFormatOpenAI)

	_, apiErr := GeminiChatStreamHandler(c, info, resp)

	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeUpstreamReject, apiErr.GetErrorCode())
	assert.Empty(t, recorder.Body.String(), "提交前不得写出起始空块或错误帧")
}

func TestGeminiChatStreamPassesThroughErrorFrameAfterCommit(t *testing.T) {
	c, _, resp, info := newGeminiStreamRejectContext(t, geminiContentFrame+geminiErrorFrame, types.RelayFormatOpenAI)

	_, apiErr := GeminiChatStreamHandler(c, info, resp)

	require.Nil(t, apiErr, "提交后不换成员、不报错")
	assert.Equal(t, geminiErrorMessage, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
}

func TestGeminiResponsesStreamRejectsErrorFrameBeforeCommit(t *testing.T) {
	c, recorder, resp, info := newGeminiStreamRejectContext(t, geminiErrorFrame, types.RelayFormatOpenAIResponses)

	_, apiErr := GeminiResponsesStreamHandler(c, info, resp)

	require.NotNil(t, apiErr, "不能把提交前的错误帧改写成会写客户端的 response.failed")
	assert.Equal(t, types.ErrorCodeUpstreamReject, apiErr.GetErrorCode())
	assert.Empty(t, recorder.Body.String())
}
