package helper

import (
	"encoding/json"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// 内容帧判定（routing-spec §4.1.1，规范单处）。
//
// 判定采用**结构帧黑名单**：只列出确定"不带载荷"的开场/收尾帧，其余一律视为
// 内容帧。反向清单（内容白名单）漏列一个未知载荷事件就会把健康流误判为失败
// （换人重试 = 工具重复调用），而黑名单漏列的后果只是"提交得早一点"（旧行为），
// 两者风险不对称，因此必须用黑名单。

// claudeStructuralEvents Anthropic SSE 的开场/收尾帧。
var claudeStructuralEvents = map[string]bool{
	"message_start":       true,
	"content_block_start": true,
	"message_delta":       true,
	"message_stop":        true,
	"ping":                true,
}

// isContentClaudeResponse Anthropic SSE：content_block_delta（text_delta /
// input_json_delta / thinking_delta）及其余未知载荷帧都是内容；
// message_start / content_block_start / message_delta / message_stop / ping 不是。
func IsContentClaudeResponse(resp dto.ClaudeResponse) bool {
	return !claudeStructuralEvents[resp.Type]
}

// responsesStructuralEvents OpenAI Responses SSE 的开场/收尾/记账帧。
var responsesStructuralEvents = map[string]bool{
	"response.created":                      true,
	"response.queued":                       true,
	"response.in_progress":                  true,
	"response.output_item.added":            true,
	"response.output_item.done":             true,
	"response.content_part.added":           true,
	"response.content_part.done":            true,
	"response.reasoning_summary_part.added": true,
	"response.reasoning_summary_part.done":  true,
	"response.completed":                    true,
	"response.failed":                       true,
	"response.incomplete":                   true,
	"response.canceled":                     true,
	"response.cancelled":                    true,
	"response.done":                         true,
	"response.output_text.done":             true,
	"response.function_call_arguments.done": true,
	"response.refusal.done":                 true,
	"response.reasoning_summary_text.done":  true,
}

// isContentResponsesChunk OpenAI Responses SSE：所有 `*.delta` 与
// `image_generation_call.partial_image` 等载荷事件都是内容；结构帧不是。
func IsContentResponsesChunk(resp dto.ResponsesStreamResponse) bool {
	return !responsesStructuralEvents[resp.Type]
}

// isContentOpenaiChunk OpenAI chat SSE（含兼容上游的同形 chunk）。
// delta 满足其一即内容：content / tool_calls / reasoning_content / reasoning /
// annotations（OpenRouter 等流式引用）。仅 role 的开场增量、finish_reason 尾帧、
// usage 尾帧不算——这三个是 OpenAI chat 流的全部结构帧形态，按 payload 计算而
// 非事件名枚举。annotations 是**载荷**：只有它、没有正文的帧也是客户端要收的
// 内容（否则该帧会被当成"零帧空流"而触发换人重试）。
func IsContentOpenaiChunk(chunk *dto.ChatCompletionsStreamResponse) bool {
	if chunk == nil {
		return false
	}
	for i := range chunk.Choices {
		delta := &chunk.Choices[i].Delta
		if (delta.Content != nil && *delta.Content != "") ||
			len(delta.ToolCalls) > 0 ||
			(delta.ReasoningContent != nil && *delta.ReasoningContent != "") ||
			(delta.Reasoning != nil && *delta.Reasoning != "") ||
			hasJSONPayload(delta.Annotations) {
			return true
		}
	}
	return false
}

// hasJSONPayload 判断 RawMessage 是否承载真实载荷（排除空值、null 与空数组）。
func hasJSONPayload(raw json.RawMessage) bool {
	payload := nonNullJSON(raw)
	if len(payload) == 0 {
		return false
	}
	return strings.TrimSpace(string(payload)) != "[]"
}

// isContentGeminiResponse Gemini 原生 JSON 对象流：candidates[].content.parts
// 非空即内容——text / functionCall / inlineData / functionResponse /
// executableCode / codeExecutionResult 都是客户端要收的载荷；空 parts 的
// 元数据帧不算。
func IsContentGeminiResponse(resp *dto.GeminiChatResponse) bool {
	if resp == nil {
		return false
	}
	for i := range resp.Candidates {
		for j := range resp.Candidates[i].Content.Parts {
			part := &resp.Candidates[i].Content.Parts[j]
			if part.Text != "" || part.FunctionCall != nil || part.InlineData != nil ||
				part.FunctionResponse != nil || part.ExecutableCode != nil ||
				part.CodeExecutionResult != nil {
				return true
			}
		}
	}
	return false
}

// IsContentFrameRaw 流式扫描层的逐帧内容判定：按入站格式解析原始 `data` 帧，
// 命中 §4.1.1 的结构帧则不算内容。解析失败按内容计（保守——宁可交付也
// 不错杀健康流；解析失败的帧随后会走各 handler 的既有容错路径）。
func IsContentFrameRaw(format types.RelayFormat, data string) bool {
	switch format {
	case types.RelayFormatOpenAI:
		var chunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
			return true
		}
		return IsContentOpenaiChunk(&chunk)
	case types.RelayFormatClaude:
		var resp dto.ClaudeResponse
		if err := common.UnmarshalJsonStr(data, &resp); err != nil {
			return true
		}
		return IsContentClaudeResponse(resp)
	case types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		var resp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &resp); err != nil {
			return true
		}
		return IsContentResponsesChunk(resp)
	case types.RelayFormatGemini:
		var resp dto.GeminiChatResponse
		if err := common.UnmarshalJsonStr(data, &resp); err != nil {
			return true
		}
		return IsContentGeminiResponse(&resp)
	default:
		// 未知格式：按内容计（保持旧口径，不误杀）。
		return true
	}
}
