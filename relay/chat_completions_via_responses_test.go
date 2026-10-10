package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	openaichannel "github.com/zzyyyds88/PowerBarRations/relay/channel/openai"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	relayconstant "github.com/zzyyyds88/PowerBarRations/relay/constant"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	relaytypes "github.com/zzyyyds88/PowerBarRations/relaykit/types"
	"github.com/zzyyyds88/PowerBarRations/setting"
)

func TestIsResponsesEventStreamContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{name: "plain", contentType: "text/event-stream", want: true},
		{name: "mixed case with charset", contentType: "Text/Event-Stream; charset=utf-8", want: true},
		{name: "json", contentType: "application/json", want: false},
		{name: "empty", contentType: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isResponsesEventStreamContentType(tt.contentType))
		})
	}
}

func TestTextRequestViaResponsesConvertsClaudeDirectly(t *testing.T) {
	type capturedRequest struct {
		path string
		body []byte
	}
	captured := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		captured <- capturedRequest{path: r.URL.Path, body: body}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_1",
			"object":"response",
			"status":"completed",
			"model":"gpt-5.6-sol",
			"output":[{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],
			"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}
		}`))
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:              relayconstant.RelayModeChatCompletions,
		RelayFormat:            relaytypes.RelayFormatClaude,
		OriginModelName:        "gpt-5.6-sol",
		RequestConversionChain: []relaytypes.RelayFormat{relaytypes.RelayFormatClaude},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    server.URL,
			ApiKey:            "test-key",
			UpstreamModelName: "gpt-5.6-sol",
		},
	}
	adaptor := &openaichannel.Adaptor{}
	adaptor.Init(info)
	request := &dto.ClaudeRequest{
		Model:    "gpt-5.6-sol",
		Thinking: &dto.Thinking{Type: "adaptive", Display: "summarized"},
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
	}

	usage, apiErr := textRequestViaResponses(c, info, adaptor, request)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 5, usage.TotalTokens)
	assert.Equal(t, []relaytypes.RelayFormat{relaytypes.RelayFormatClaude, relaytypes.RelayFormatOpenAIResponses}, info.RequestConversionChain)

	upstream := <-captured
	assert.Equal(t, "/v1/responses", upstream.path)
	var upstreamBody map[string]any
	require.NoError(t, common.Unmarshal(upstream.body, &upstreamBody))
	assert.NotContains(t, upstreamBody, "messages")
	reasoning, ok := upstreamBody["reasoning"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "high", reasoning["effort"])
	assert.Equal(t, "detailed", reasoning["summary"])

	var response dto.ClaudeResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Content, 1)
	assert.Equal(t, "ok", response.Content[0].GetText())
}

func TestApplySystemPromptIfNeededSkipsToolLoadingMessages(t *testing.T) {
	tools := json.RawMessage(`[{"type":"function","function":{"name":"get_current_time","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]`)
	toolLoading := dto.Message{Role: "system", Tools: tools}
	user := dto.Message{Role: "user", Content: "What time is it in Beijing?"}

	tests := []struct {
		name         string
		messages     []dto.Message
		wantMessages []dto.Message
		wantOverride bool
	}{
		{
			name:     "tool loading message alone is not a system prompt",
			messages: []dto.Message{toolLoading, user},
			wantMessages: []dto.Message{
				{Role: "system", Content: "Answer in English."},
				toolLoading,
				user,
			},
		},
		{
			name:     "override targets the real system prompt only",
			messages: []dto.Message{toolLoading, {Role: "system", Content: "You are Kimi."}, user},
			wantMessages: []dto.Message{
				toolLoading,
				{Role: "system", Content: "Answer in English.\nYou are Kimi."},
				user,
			},
			wantOverride: true,
		},
	}

	// 该组用例验证「开关打开时」的注入语义；默认关闭的行为另有用例。
	setting.ChannelSystemPromptEnabled = true
	t.Cleanup(func() { setting.ChannelSystemPromptEnabled = false })

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelSetting: dto.ChannelSettings{
						SystemPrompt:         "Answer in English.",
						SystemPromptOverride: true,
					},
				},
			}
			request := &dto.GeneralOpenAIRequest{
				Model:    "kimi-k3",
				Messages: append([]dto.Message(nil), tt.messages...),
			}

			applySystemPromptIfNeeded(c, info, request)

			require.Len(t, request.Messages, len(tt.wantMessages))
			for i, want := range tt.wantMessages {
				got := request.Messages[i]
				assert.Equal(t, want.Role, got.Role, "message %d role", i)
				assert.Equal(t, want.Content, got.Content, "message %d content", i)
				if len(want.Tools) > 0 {
					assert.JSONEq(t, string(want.Tools), string(got.Tools), "message %d tools", i)
				} else {
					assert.Empty(t, got.Tools, "message %d tools", i)
				}
			}
			_, overrideSet := common.GetContextKey(c, constant.ContextKeySystemPromptOverride)
			assert.Equal(t, tt.wantOverride, overrideSet)
		})
	}
}

// 渠道系统提示词注入**默认关闭**（2026-10-10 董事长令）：关着时即使渠道配了
// system_prompt / override，请求也一个字节都不改，且不置 override 上下文。
func TestChannelSystemPromptInjectionIsOffByDefault(t *testing.T) {
	setting.ChannelSystemPromptEnabled = false

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{
				SystemPrompt:         "Answer in English.",
				SystemPromptOverride: true,
			},
		},
	}

	// 情形一：请求本来没有 system 消息（关闭时不得前插）
	plain := &dto.GeneralOpenAIRequest{
		Model:    "m",
		Messages: []dto.Message{{Role: "user", Content: "hi"}},
	}
	applySystemPromptIfNeeded(c, info, plain)
	require.Len(t, plain.Messages, 1)
	assert.Equal(t, "user", plain.Messages[0].Role)
	assert.Equal(t, "hi", plain.Messages[0].Content)

	// 情形二：请求已有 system 消息（关闭时不得拼接）
	withSystem := &dto.GeneralOpenAIRequest{
		Model: "m",
		Messages: []dto.Message{
			{Role: "system", Content: "You are Kimi."},
			{Role: "user", Content: "hi"},
		},
	}
	applySystemPromptIfNeeded(c, info, withSystem)
	assert.Equal(t, "You are Kimi.", withSystem.Messages[0].Content)

	_, overrideSet := common.GetContextKey(c, constant.ContextKeySystemPromptOverride)
	assert.False(t, overrideSet, "关闭时不得置 override 上下文")
}
