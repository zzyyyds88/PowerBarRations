package helper

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/logger"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func FlushWriter(c *gin.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("flush panic recovered: %v", r)
		}
	}()

	if c == nil || c.Writer == nil {
		return nil
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}

	flusher.Flush()
	return nil
}

func requestContextDone(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.Request.Context().Err() != nil
}

func SetEventStreamHeaders(c *gin.Context) {
	// 检查是否已经设置过头部
	if _, exists := c.Get("event_stream_headers_set"); exists {
		return
	}

	// 设置标志，表示头部已经设置过
	c.Set("event_stream_headers_set", true)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}

func ClaudeData(c *gin.Context, resp dto.ClaudeResponse) error {
	if requestContextDone(c) {
		return nil
	}

	jsonData, err := common.Marshal(resp)
	if err != nil {
		common.SysError("error marshalling stream response: " + err.Error())
		return nil
	}
	// 提交点 = 内容帧（routing-spec §3 第 6 步 / §4.1.1）：开场帧先暂存，
	// 内容帧到达时按序 flush 并提交；内容帧之前出现流内错误帧或 EOF 则
	// 未提交（成员失败 + 故障转移，暂存帧丢弃）。
	return writeStreamEvent(c, IsContentClaudeResponse(resp),
		fmt.Sprintf("event: %s\n", resp.Type), "data: "+string(jsonData))
}

// 数据帧写入口。三个函数写出的是真正携带内容的 `data:` 帧，因此都要打"已提交"
// 标记（routing-spec §3 第 6 步）：它是流式故障转移的提交判定，心跳（PingData 的
// `: PING` 注释帧）与响应头不打标记——客户端可忽略注释帧，发过心跳不等于已交付。
func ClaudeChunkData(c *gin.Context, resp dto.ClaudeResponse, data string) {
	if requestContextDone(c) {
		return
	}

	_ = writeStreamEvent(c, IsContentClaudeResponse(resp),
		fmt.Sprintf("event: %s\n", resp.Type), "data: "+data+"\n")
}

func ResponseChunkData(c *gin.Context, resp dto.ResponsesStreamResponse, data string) error {
	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	return writeStreamEvent(c, IsContentResponsesChunk(resp),
		fmt.Sprintf("event: %s\n", resp.Type), "data: "+data)
}

func StringData(c *gin.Context, str string) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	// 通用写入口（Gemini 原生、legacy 适配器、[DONE] 等）：无逐格式判定可用，
	// 维持"数据帧即提交"的旧口径——与 §4.1.1 的差异仅限这些 niche 路径，
	// 主入站格式（openai chat / responses / anthropic）各自有类型化判定。
	if err := flushHeldStreamFrames(c); err != nil {
		return err
	}
	c.Render(-1, common.CustomEvent{Data: "data: " + str})
	MarkStreamCommitted(c)
	return FlushWriter(c)
}

func PingData(c *gin.Context) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	if _, err := c.Writer.Write([]byte(": PING\n\n")); err != nil {
		return fmt.Errorf("write ping data failed: %w", err)
	}
	return FlushWriter(c)
}

func ObjectData(c *gin.Context, object any) error {
	if object == nil {
		return errors.New("object is nil")
	}
	// 类型化内容判定（routing-spec §4.1.1）：主入站格式的流式对象在此判
	// "是否携带载荷"，未提交时开场帧走暂存；其余对象类型（legacy 适配器、
	// 非流式响应体）维持"数据帧即提交"的通用口径。
	isContent := true
	switch v := object.(type) {
	case *dto.ChatCompletionsStreamResponse:
		isContent = IsContentOpenaiChunk(v)
	case dto.ChatCompletionsStreamResponse:
		isContent = IsContentOpenaiChunk(&v)
	case *dto.GeminiChatResponse:
		isContent = IsContentGeminiResponse(v)
	case dto.GeminiChatResponse:
		isContent = IsContentGeminiResponse(&v)
	}
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	return StringDataGated(c, string(jsonData), isContent)
}

func Done(c *gin.Context) {
	_ = StringData(c, "[DONE]")
}

func WssString(c *gin.Context, ws *websocket.Conn, str string) error {
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", str))
	return ws.WriteMessage(1, []byte(str))
}

func WssObject(c *gin.Context, ws *websocket.Conn, object any) error {
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", jsonData))
	return ws.WriteMessage(1, jsonData)
}

func WssError(c *gin.Context, ws *websocket.Conn, openaiError types.OpenAIError) {
	if ws == nil {
		return
	}
	errorObj := &dto.RealtimeEvent{
		Type:    "error",
		EventId: GetLocalRealtimeID(c),
		Error:   &openaiError,
	}
	_ = WssObject(c, ws, errorObj)
}

func GetResponseID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("chatcmpl-%s", logID)
}

func GetLocalRealtimeID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("evt_%s", logID)
}

func GenerateStartEmptyResponse(id string, createAt int64, model string, systemFingerprint *string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role:    "assistant",
					Content: common.GetPointer(""),
				},
			},
		},
	}
}

func GenerateStopResponse(id string, createAt int64, model string, finishReason string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				FinishReason: &finishReason,
			},
		},
	}
}

func GenerateFinalUsageResponse(id string, createAt int64, model string, usage dto.Usage) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices:           make([]dto.ChatCompletionsStreamResponseChoice, 0),
		Usage:             &usage,
	}
}
