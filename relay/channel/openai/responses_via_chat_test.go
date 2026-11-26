package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/constant"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// 跨格式流式出口必须统一按 StreamOutcomeError 收尾（ADR 0013 第 4 条、
// routing-spec §4.1/§4.3）：客户端取消、首事件超时、读流失败与零内容帧
// 空流各按真实原因分类，未提交时不得返回 200。

var errStreamReadFailure = errors.New("read tcp: connection reset by peer")

type failingStreamReader struct{ err error }

func (r failingStreamReader) Read([]byte) (int, error) { return 0, r.err }
func (r failingStreamReader) Close() error             { return nil }

func withStreamingTimeout(t *testing.T, seconds int) {
	t.Helper()
	old := constant.StreamingTimeout
	constant.StreamingTimeout = seconds
	t.Cleanup(func() { constant.StreamingTimeout = old })
}

type streamHandlerFunc func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)

type streamOutcomeCase struct {
	name    string
	prepare func(t *testing.T) (io.ReadCloser, context.Context)
	check   func(t *testing.T, apiErr *types.NewAPIError)
}

func streamOutcomeCases() []streamOutcomeCase {
	return []streamOutcomeCase{
		{
			name: "client_gone",
			prepare: func(t *testing.T) (io.ReadCloser, context.Context) {
				withStreamingTimeout(t, 30)
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				cancel()
				return io.NopCloser(strings.NewReader("")), ctx
			},
			check: func(t *testing.T, apiErr *types.NewAPIError) {
				assert.ErrorIs(t, apiErr, context.Canceled)
				assert.NotEqual(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode(),
					"客户端取消不能归成空响应")
			},
		},
		{
			name: "timeout",
			prepare: func(t *testing.T) (io.ReadCloser, context.Context) {
				// 流一直不产出事件：首事件/空闲超时是唯一出口。
				withStreamingTimeout(t, 1)
				pr, pw := io.Pipe()
				t.Cleanup(func() { _ = pw.Close() })
				return pr, context.Background()
			},
			check: func(t *testing.T, apiErr *types.NewAPIError) {
				assert.ErrorIs(t, apiErr, context.DeadlineExceeded)
				assert.Equal(t, types.ErrorCodeChannelResponseTimeExceeded, apiErr.GetErrorCode())
			},
		},
		{
			name: "scanner_error",
			prepare: func(t *testing.T) (io.ReadCloser, context.Context) {
				withStreamingTimeout(t, 30)
				return io.NopCloser(failingStreamReader{err: errStreamReadFailure}), context.Background()
			},
			check: func(t *testing.T, apiErr *types.NewAPIError) {
				assert.ErrorIs(t, apiErr, errStreamReadFailure)
				assert.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
				assert.NotEqual(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode(),
					"scanner 错误必须按错误链分类，不能被空流分类顶替")
			},
		},
		{
			name: "normal_zero_frame",
			prepare: func(t *testing.T) (io.ReadCloser, context.Context) {
				withStreamingTimeout(t, 30)
				return io.NopCloser(strings.NewReader("data: [DONE]\n")), context.Background()
			},
			check: func(t *testing.T, apiErr *types.NewAPIError) {
				assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode(),
					"正常结束但零内容帧是空响应")
			},
		},
	}
}

func runStreamOutcomeCases(t *testing.T, handler streamHandlerFunc, cases []streamOutcomeCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder, resp, info := newResponsesChatTestContext(t, "", true)
			body, ctx := tc.prepare(t)
			resp.Body = body
			c.Request = c.Request.WithContext(ctx)

			usage, apiErr := handler(c, info, resp)

			require.Nil(t, usage, "失败尝试不返回 usage")
			require.NotNil(t, apiErr, "未提交的流式失败必须返回错误，不能以 200 成功收尾")
			tc.check(t, apiErr)
			assert.Empty(t, recorder.Body.String(), "未提交失败不写任何帧给客户端")
		})
	}
}

func TestOaiChatToResponsesStreamHandlerClassifiesStreamOutcome(t *testing.T) {
	runStreamOutcomeCases(t, OaiChatToResponsesStreamHandler, streamOutcomeCases())
}

// 上游用流内错误对象拒绝且失败终态事件已写出时，handler 仍须以失败收尾：
// 此前 sr.Stop(nil) 会记成 handler_stop，把失败吞成成功（ADR 0013 第 4 条）。
func TestOaiChatToResponsesStreamHandlerMarksFailureAfterCommittedStreamError(t *testing.T) {
	withStreamingTimeout(t, 30)

	body := strings.Join([]string{
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
		`data: {"error":{"message":"content blocked","type":"server_error"}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr, "失败终态事件写出后本次尝试仍须记失败")
	openAIErr := apiErr.ToOpenAIError()
	assert.Equal(t, "server_error", openAIErr.Type, "保留原始上游错误对象")
	assert.Equal(t, "content blocked", openAIErr.Message)
	assert.Contains(t, recorder.Body.String(), "hi")
	assert.Contains(t, recorder.Body.String(), "event: response.failed",
		"既有响应格式不变：仍按 Responses 协议写失败终态事件")
}
