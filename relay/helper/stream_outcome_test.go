package helper

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/internal/route"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 流式收尾必须按 EndReason 分类，不得一律顶替成 bad_response（ADR 0013 第 4 条）。
func TestStreamOutcomeErrorClassifiesEndReason(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name      string
		reason    relaycommon.StreamEndReason
		endErr    error
		received  int
		committed bool
		wantKind  route.ErrorKind
		wantNil   bool
	}{
		{
			name:     "超时归 timeout",
			reason:   relaycommon.StreamEndReasonTimeout,
			wantKind: route.KindTimeout,
		},
		{
			name:     "客户端断开归 canceled",
			reason:   relaycommon.StreamEndReasonClientGone,
			endErr:   context.Canceled,
			wantKind: route.KindCanceled,
		},
		{
			name:     "scanner 错误归 bad_response",
			reason:   relaycommon.StreamEndReasonScannerErr,
			endErr:   errors.New("read tcp: connection reset by peer"),
			wantKind: route.KindBadResponse,
		},
		{
			name:     "scanner 的 deadline 仍归 timeout",
			reason:   relaycommon.StreamEndReasonScannerErr,
			endErr:   context.DeadlineExceeded,
			wantKind: route.KindTimeout,
		},
		{
			name:     "panic 归 bad_response",
			reason:   relaycommon.StreamEndReasonPanic,
			wantKind: route.KindBadResponse,
		},
		{
			name:     "ping 失败归 soft_transient",
			reason:   relaycommon.StreamEndReasonPingFail,
			wantKind: route.KindSoftTransient,
		},
		{
			name:     "正常结束且有内容帧不改判",
			reason:   relaycommon.StreamEndReasonDone,
			received: 3,
			wantNil:  true,
		},
		{
			name:     "正常结束但零内容帧归 bad_response（空流）",
			reason:   relaycommon.StreamEndReasonDone,
			wantKind: route.KindBadResponse,
		},
		{
			name:      "已提交后超时同样返回错误供日志留痕",
			reason:    relaycommon.StreamEndReasonTimeout,
			received:  1,
			committed: true,
			wantKind:  route.KindTimeout,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tc.committed {
				MarkStreamCommitted(c)
			}
			info := &relaycommon.RelayInfo{
				StreamStatus:         relaycommon.NewStreamStatus(),
				ReceivedContentCount: tc.received,
			}
			info.StreamStatus.SetEndReason(tc.reason, tc.endErr)

			err := StreamOutcomeError(c, info)
			if tc.wantNil {
				assert.Nil(t, err)
				return
			}
			require.NotNil(t, err)
			assert.Equal(t, tc.wantKind, route.Classify(err))
			if tc.reason == relaycommon.StreamEndReasonDone && tc.received == 0 {
				assert.Equal(t, types.ErrorCodeEmptyResponse, err.GetErrorCode(),
					"零内容帧空流复用 EmptyResponse，归 bad_response")
			}
		})
	}
}

func TestStreamOutcomeErrorNilInfo(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.Nil(t, StreamOutcomeError(c, nil))
}

// 内容帧判定必须覆盖新增载荷字段，避免合法内容被当成"零帧空流"。
func TestContentFrameCoversAnnotationsAndGeminiPayloads(t *testing.T) {
	openAI := &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				Annotations: []byte(`[{"type":"url_citation","url":"https://example.test"}]`),
			},
		}},
	}
	assert.True(t, IsContentOpenaiChunk(openAI), "只有 annotations 的帧也是内容")

	roleOnly := &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"},
		}},
	}
	assert.False(t, IsContentOpenaiChunk(roleOnly), "纯角色开场帧仍不是内容")

	gemini := &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{ExecutableCode: &dto.GeminiPartExecutableCode{}}}},
		}},
	}
	assert.True(t, IsContentGeminiResponse(gemini), "executableCode 是内容载荷")

	geminiFunctionResponse := &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{FunctionResponse: &dto.GeminiFunctionResponse{}}}},
		}},
	}
	assert.True(t, IsContentGeminiResponse(geminiFunctionResponse), "functionResponse 是内容载荷")
}
