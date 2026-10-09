package helper

import (
	"encoding/json"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"

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
// 只识别两种形态（与图像流路径 isOpenAIImageStreamErrorEvent 同口径，避免误判）：
//  1. 含**非空** `error` 对象（`{"error":{...}}`；`"error": null` 不算）；
//  2. `type` 字段为 `error` / `upstream_error`。
//
// 返回 (上游报文, 是否错误帧)。报文优先取顶层 `message`，其次 `error.message`，
// 再回落 `error` 原文，最终回落固定文案。
func IsUpstreamErrorFrame(data []byte) (string, bool) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || !json.Valid([]byte(trimmed)) {
		return "", false
	}
	var payload struct {
		Type    string          `json:"type"`
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := common.UnmarshalJsonStr(trimmed, &payload); err != nil {
		return "", false
	}
	payloadType := strings.ToLower(strings.TrimSpace(payload.Type))
	hasErrorField := len(payload.Error) > 0 && !isJSONNull(payload.Error)
	if payloadType != "error" && payloadType != "upstream_error" && !hasErrorField {
		return "", false
	}
	if msg := strings.TrimSpace(payload.Message); msg != "" {
		return msg, true
	}
	if hasErrorField {
		var nested struct {
			Message string `json:"message"`
		}
		if err := common.Unmarshal(payload.Error, &nested); err == nil {
			if msg := strings.TrimSpace(nested.Message); msg != "" {
				return msg, true
			}
		}
		if raw := strings.TrimSpace(common.JsonRawMessageToString(payload.Error)); raw != "" {
			return raw, true
		}
	}
	return "upstream stream returned error event", true
}

// isJSONNull 判断原始 JSON 片段是否就是 null。
func isJSONNull(raw json.RawMessage) bool {
	return strings.EqualFold(strings.TrimSpace(string(raw)), "null")
}

// MarkStreamCommitted 标记"本请求已向客户端写出数据帧"。只在真正写出
// `data:` 帧的写入口调用（StringData / ResponseChunkData / ClaudeChunkData）；
// 心跳（`: PING`）与响应头不算提交（routing-spec §3 第 6 步）。
func MarkStreamCommitted(c *gin.Context) {
	if c == nil {
		return
	}
	common.SetContextKey(c, constant.ContextKeyPBRStreamCommitted, true)
}

// StreamCommitted 本请求是否已向客户端提交过数据帧（routing-spec §4.4 的
// "提交前/提交后"判定）。
func StreamCommitted(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return common.GetContextKeyBool(c, constant.ContextKeyPBRStreamCommitted)
}
