package helper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
)

// 本文件承载"流内错误帧"的共享判定与流式提交标记（routing-spec §4.4）。
//
// 背景：部分上游先以 `200 + text/event-stream` 建流，再把拒绝写进流里（内容审查
// 类尤为典型）。这类失败在传输层是 200，若不识别就会被当作普通流透传给下游，
// 车道既不能换成员、日志也看不到失败。判定必须保守（只认明确的 error 帧形态），
// 宁可漏判也不能把正常内容误判成错误。

// IsUpstreamErrorFrame 判断一个 SSE `data:` 帧是否为上游错误帧。
//
// 全部入站格式共享这一个探测器（routing-spec §4.4），只认明确的形态：
//  1. 含**非空** `error` 对象（`{"error":{...}}`；`"error": null` 不算）——
//     OpenAI chat 与 Gemini 流式 JSON 帧的典型形态；
//  2. `type` 字段为 `error` / `upstream_error`——OpenAI chat / Responses 的形态。
//     Anthropic SSE 的 `event: error` 与 data 内的 `type=error` 是同义信息，
//     扫描器只投递 `data:` 帧，因此按 type 识别即覆盖该事件；
//  3. Responses：`type=response.failed` **且** `response.error` 非空——这是
//     Responses 协议把失败写进流里的形态；不带 error 的 `response.failed`
//     是正常终态事件，客户端要靠它收尾，不能判成错误帧。
//
// 心跳、注释帧、`[DONE]`、`error: null` 一律不算——宁可漏判也不误判。
// 返回 (上游报文, 是否错误帧)。报文优先取顶层 `message`、`error.message`、
// `response.error.message`，再回落 `error` / `response.error` 原文，最终回落固定文案。
func IsUpstreamErrorFrame(data []byte) (string, bool) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || !json.Valid([]byte(trimmed)) {
		return "", false
	}
	var payload struct {
		Type     string          `json:"type"`
		Message  string          `json:"message"`
		Error    json.RawMessage `json:"error"`
		Response json.RawMessage `json:"response"`
	}
	if err := common.UnmarshalJsonStr(trimmed, &payload); err != nil {
		return "", false
	}
	payloadType := strings.ToLower(strings.TrimSpace(payload.Type))
	errorField := nonNullJSON(payload.Error)
	responsesError := responsesFrameError(payloadType, payload.Response)
	if payloadType != "error" && payloadType != "upstream_error" && len(errorField) == 0 && len(responsesError) == 0 {
		return "", false
	}
	if msg := strings.TrimSpace(payload.Message); msg != "" {
		return msg, true
	}
	if msg := nestedErrorMessage(errorField); msg != "" {
		return msg, true
	}
	if msg := nestedErrorMessage(responsesError); msg != "" {
		return msg, true
	}
	return "upstream stream returned error event", true
}

// responsesFrameError 取 `type=response.failed` 帧里的 `response.error`。
// 只有该字段非空时才是错误帧；其他 type 或空值一律返回 nil。
func responsesFrameError(payloadType string, response json.RawMessage) json.RawMessage {
	if payloadType != "response.failed" {
		return nil
	}
	responseField := nonNullJSON(response)
	if len(responseField) == 0 {
		return nil
	}
	var failed struct {
		Error json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(responseField, &failed); err != nil {
		return nil
	}
	return nonNullJSON(failed.Error)
}

// nonNullJSON 去掉空值与 JSON null：`"error": null` / `"response": null`
// 是"没有错误"，不能当成错误帧（routing-spec §4.4）。
func nonNullJSON(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return nil
	}
	return raw
}

// nestedErrorMessage 从一个错误值里取报文：对象取 `message`，字符串取其原文，
// 其余形态回落到原始 JSON 文本。
func nestedErrorMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var nested struct {
		Message string `json:"message"`
	}
	if err := common.Unmarshal(raw, &nested); err == nil {
		if msg := strings.TrimSpace(nested.Message); msg != "" {
			return msg
		}
	}
	return strings.TrimSpace(common.JsonRawMessageToString(raw))
}

// NewUpstreamStreamRejectError 把提交前流内错误帧的上游报文包装成一次成员级拒绝失败
// （routing-spec §4.1/§4.4）：车道据此换下一成员、同一成员不原地重试、不写冷却、
// 不计熔断分。四条入站流式路径共用这一个构造，不各自拼一条错误链。
func NewUpstreamStreamRejectError(msg string) *types.NewAPIError {
	return types.NewOpenAIError(
		fmt.Errorf("upstream stream rejected: %s", msg),
		types.ErrorCodeUpstreamReject,
		http.StatusBadRequest,
	)
}

// MarkStreamReject 记录提交后收到的流内 error 帧报文（routing-spec §4.3/§4.4）。
//
// 此时传输层已是 200 且内容已交付，不能故障转移；日志收尾读到该键就
// 记 success=true + error_kind=upstream_reject + 帧内报文，让这类"客户端拿到
// 200 流、流内却被上游判拒"的失败可被检索与统计。
func MarkStreamReject(c *gin.Context, msg string) {
	if c == nil || msg == "" {
		return
	}
	common.SetContextKey(c, constant.ContextKeyPBRStreamReject, msg)
}

// MarkStreamCommitted 标记"本请求已向客户端写出数据帧"。只在真正写出
// `data:` 帧的写入口调用（StringData / ResponseChunkData / ClaudeChunkData）；
// 心跳（`: PING`）与响应头不算提交（routing-spec §3 第 6 步）。
func MarkStreamCommitted(c *gin.Context) {
	if c == nil {
		return
	}
	MarkFirstByte(c)
	common.SetContextKey(c, constant.ContextKeyPBRStreamCommitted, true)
}

// MarkFirstByte 记录首字耗时（TTFT）：从请求起始到第一帧数据写出。
//
// 幂等——只在第一次写入；请求起始时间取不到时留空（不猜）。
// 挂在这里是因为这三个写入口就是"第一帧数据"的唯一通道，且都只用于流式响应，
// 非流式请求不会误记。
func MarkFirstByte(c *gin.Context) {
	if c == nil {
		return
	}
	if common.GetContextKeyInt(c, constant.ContextKeyPBRTTFTMs) > 0 {
		return
	}
	start := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if start.IsZero() {
		return
	}
	if ms := time.Since(start).Milliseconds(); ms > 0 {
		common.SetContextKey(c, constant.ContextKeyPBRTTFTMs, int(ms))
	}
}

// StreamCommitted 本请求是否已向客户端提交过数据帧（routing-spec §4.4 的
// "提交前/提交后"判定）。
func StreamCommitted(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return common.GetContextKeyBool(c, constant.ContextKeyPBRStreamCommitted)
}

// StreamFailedWithoutFrames 本次流式尝试是否属于"上游已建流但零有效帧"。
//
// 传输层拿到 200 + text/event-stream，但上游一帧内容都没发（读超时、立即 EOF、
// 只有 `[DONE]`）——这是上游的空响应，不是成功。此前各 handler 收尾时把
// "末帧解析失败"只写日志就吞掉，于是这种请求以 200 + success=true + 零输出落库，
// 客户端干等到超时，车道运行态完全没记这次失败（routing-spec §4.1 把它归为
// 上游空响应）。
//
// 判定必须晚于"是否有帧"的统计、早于任何终态帧写出：已提交（客户端已收到
// 数据）时不能改判，否则会把正常流截成错误。
func StreamFailedWithoutFrames(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	// 内容帧口径（routing-spec §3 第 6 步 / §4.1.1）：提交 ⟺ 已写出内容帧。
	// 仅角色/开场帧的不算——那正是"HTTP 200 空流"的形态。
	// 收到过内容帧但尚未写出的（末帧在守卫之后按既有设计转发）同样不算空流。
	if info.ReceivedContentCount > 0 || StreamCommitted(c) {
		return false
	}
	return true
}

// EmptyStreamError 把"零帧空流"包装成成员级失败。
//
// 复用 ErrorCodeEmptyResponse：internal/route 已把它归入 bad_response，
// 因而自动获得"同成员重试 + 换成员 + 冷却"语义，不需要改错误分类器。
func EmptyStreamError() *types.NewAPIError {
	return types.NewError(
		fmt.Errorf("upstream stream ended without any data frame"),
		types.ErrorCodeEmptyResponse,
	)
}
