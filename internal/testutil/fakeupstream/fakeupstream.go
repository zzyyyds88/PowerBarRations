// Package fakeupstream 是 PowerBarRations 的内置假上游。
//
// 用途：failover / 冷却 / 熔断 / 超时 / 故障注入的验收一律打它，绝不打真实厂商
// （施工铁律 5）。同一份实现既供 Go 测试进程内使用（New），也供 verify/*.sh
// 以独立进程运行（参见 cmd/fakeupstream）。
//
// 它按"收到的 model + Authorization"决定行为，并记录每一次请求体，便于断言
// 网关到底把什么发给了上游（例如成员改名、param_override 合并结果）。
package fakeupstream

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// Behavior 对某个模型的定制行为；零值表示"正常回包"。
type Behavior struct {
	Status int           // 0 = 200
	Body   string        // 非空则原样返回（用于坏响应/欠费关键词等注入）
	Delay  time.Duration // 首包延迟
	// Mode 特殊故障模式：
	//   "bad_json"         200 但响应体不是合法 JSON（bad_response 分类）
	//   "disconnect_stream" 流式发出两帧后直接断开连接（模拟流中途断流）
	//   "empty"            200 但响应体为空
	Mode string
}

// Config 假上游配置。
type Config struct {
	// RequireKey 非空时校验 Authorization: Bearer <RequireKey>，不匹配返回 401。
	RequireKey string
	// Models 非空时只接受列表内的模型名，其余返回 404。
	Models []string
	// Behaviors 按请求里的 model 定制响应（故障注入）。
	Behaviors map[string]Behavior
	// LogWriter 非空时，把每次请求以一行 JSON 写入（独立进程模式用）。
	LogWriter io.Writer
}

// RecordedRequest 一次收到的请求。
type RecordedRequest struct {
	Time          time.Time `json:"time"`
	Method        string    `json:"method"`
	Path          string    `json:"path"`
	Model         string    `json:"model"`
	Authorization string    `json:"authorization"`
	Body          string    `json:"body"`
}

// controlPath 运行时控制端点：验收脚本用它注入故障与"修好"。
//
//	POST /__control  {"model":"model-1","status":500}      让该模型必然返回 500
//	POST /__control  {"model":"model-1","status":200}      修好（恢复正常回包）
//	POST /__control  {"model":"*","status":500}            所有模型
const controlPath = "/__control"

// Server 进程内假上游（httptest）。
type Server struct {
	*httptest.Server

	cfg Config

	mu   sync.Mutex
	reqs []RecordedRequest
}

// New 启动一个进程内假上游，测试结束自动关闭。
func New(cfg Config) *Server {
	s := &Server{cfg: cfg}
	s.Server = httptest.NewServer(s)
	return s
}

// Requests 返回收到的全部请求（副本）。
func (s *Server) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RecordedRequest(nil), s.reqs...)
}

// LastRequest 返回最后一次请求；无请求时 ok=false。
func (s *Server) LastRequest() (RecordedRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		return RecordedRequest{}, false
	}
	return s.reqs[len(s.reqs)-1], true
}

// Reset 清空记录。
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if r.URL.Path == controlPath {
		s.handleControl(w, body)
		return
	}
	modelName := extractModel(body)
	record := RecordedRequest{
		Time:          time.Now().UTC(),
		Method:        r.Method,
		Path:          r.URL.Path,
		Model:         modelName,
		Authorization: r.Header.Get("Authorization"),
		Body:          string(body),
	}

	s.mu.Lock()
	s.reqs = append(s.reqs, record)
	logWriter := s.cfg.LogWriter
	s.mu.Unlock()

	if r.URL.Path == controlPath {
		s.handleControl(w, body)
		return
	}

	if logWriter != nil {
		if encoded, err := json.Marshal(record); err == nil {
			line := append(encoded, '\n')
			_, _ = logWriter.Write(line)
		}
	}

	behavior := s.behaviorFor(modelName)
	if behavior.Delay > 0 {
		time.Sleep(behavior.Delay)
	}

	if s.cfg.RequireKey != "" && authorizationKey(r) != s.cfg.RequireKey {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]any{
				"message": "invalid api key",
				"type":    "invalid_request_error",
				"code":    "invalid_api_key",
			},
		})
		return
	}

	switch behavior.Mode {
	case "bad_json":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("this-is-not-json-at-all"))
		return
	case "empty":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	case "disconnect_stream":
		writeChatStreamThenDisconnect(w, modelName)
		return
	}

	if behavior.Status != 0 && behavior.Status != http.StatusOK {
		if behavior.Body != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(behavior.Status)
			_, _ = w.Write([]byte(behavior.Body))
			return
		}
		writeJSON(w, behavior.Status, map[string]any{"error": map[string]any{"message": http.StatusText(behavior.Status)}})
		return
	}

	if behavior.Body != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(behavior.Body))
		return
	}

	if len(s.cfg.Models) > 0 && !contains(s.cfg.Models, modelName) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"message": "model not found"}})
		return
	}

	// 模型清单：供管理面的 sync-models 验收
	if strings.HasSuffix(r.URL.Path, "/models") && r.Method == http.MethodGet {
		list := s.cfg.Models
		if len(list) == 0 {
			list = []string{"listed-model-a", "listed-model-b"}
		}
		items := make([]map[string]any, 0, len(list))
		for _, id := range list {
			items = append(items, map[string]any{"id": id, "object": "model"})
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": items})
		return
	}

	// 嵌入端点：返回确定性向量，长度与输入条数一致
	if strings.HasSuffix(r.URL.Path, "/embeddings") {
		writeJSON(w, http.StatusOK, embeddingsResponse(modelName, body))
		return
	}

	// 请求里带 tools 时回一个 tool_calls，用于验证网关对工具调用的透传
	if hasTools(body) {
		writeJSON(w, http.StatusOK, toolCallCompletion(modelName))
		return
	}

	if wantsStream(body) {
		writeChatStream(w, modelName, markerOf(body))
		return
	}
	writeJSON(w, http.StatusOK, chatCompletion(modelName, markerOf(body)))
}

// Handler 返回可挂到任意 http.Server 的处理器（独立进程模式）。
func Handler(cfg Config) http.Handler {
	s := &Server{cfg: cfg}
	return s
}

// handleControl 运行时改写某模型的行为（故障注入 / 修复）。
func (s *Server) handleControl(w http.ResponseWriter, body []byte) {
	var command struct {
		Model   string `json:"model"`
		Status  int    `json:"status"`
		Body    string `json:"body"`
		DelayMs int    `json:"delay_ms"`
		Mode    string `json:"mode"`
	}
	if err := json.Unmarshal(body, &command); err != nil || command.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "expect {\"model\",\"status\"}"}})
		return
	}
	s.mu.Lock()
	if s.cfg.Behaviors == nil {
		s.cfg.Behaviors = map[string]Behavior{}
	}
	s.cfg.Behaviors[command.Model] = Behavior{
		Status: command.Status,
		Body:   command.Body,
		Delay:  time.Duration(command.DelayMs) * time.Millisecond,
		Mode:   command.Mode,
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "model": command.Model, "status": command.Status})
}

func (s *Server) behaviorFor(modelName string) Behavior {
	s.mu.Lock()
	defer s.mu.Unlock()
	if behavior, ok := s.cfg.Behaviors[modelName]; ok {
		return behavior
	}
	return s.cfg.Behaviors["*"]
}

func authorizationKey(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return strings.TrimSpace(r.Header.Get("X-Api-Key"))
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(auth)
}

func extractModel(body []byte) string {
	var probe struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	return probe.Model
}

func wantsStream(body []byte) bool {
	var probe struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.Stream
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func hasTools(body []byte) bool {
	var probe struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return len(probe.Tools) > 0
}

// toolCallCompletion 回一个工具调用，内容与入参无关但结构完整。
func toolCallCompletion(modelName string) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-fake-tool",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   modelName,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]any{{
					"id":   "call_fake_1",
					"type": "function",
					"function": map[string]any{
						"name":      "get_weather",
						"arguments": `{"city":"Shanghai"}`,
					},
				}},
			},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5},
	}
}

// embeddingsResponse 返回确定性向量：便于断言"网关把嵌入请求原样转出去了"。
func embeddingsResponse(modelName string, body []byte) map[string]any {
	var probe struct {
		Input any `json:"input"`
	}
	_ = json.Unmarshal(body, &probe)
	count := 1
	switch value := probe.Input.(type) {
	case []any:
		count = len(value)
	case string:
		count = 1
	}
	data := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		data = append(data, map[string]any{
			"object":    "embedding",
			"index":     i,
			"embedding": []float64{0.1, 0.2, 0.3},
		})
	}
	return map[string]any{
		"object": "list",
		"data":   data,
		"model":  modelName,
		"usage":  map[string]any{"prompt_tokens": count, "total_tokens": count},
	}
}

func chatCompletion(modelName string, marker string) map[string]any {
	// 回显请求里的标记：并发验收据此判断"响应是不是本请求的响应"（跨请求串号检测）。
	content := "pong from fake upstream"
	if marker != "" {
		content = content + "|" + marker
	}
	return map[string]any{
		"id":      "chatcmpl-fake",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   modelName,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	}
}

// markerOf 取请求里最后一条 user 消息的内容（原样回显用）。
func markerOf(body []byte) string {
	var probe struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	for i := len(probe.Messages) - 1; i >= 0; i-- {
		if probe.Messages[i].Role != "user" {
			continue
		}
		if text, ok := probe.Messages[i].Content.(string); ok {
			return text
		}
	}
	return ""
}

func writeChatStream(w http.ResponseWriter, modelName string, marker string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	chunk := func(delta map[string]any, finish any) {
		payload := map[string]any{
			"id":      "chatcmpl-fake",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   modelName,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
		}
		encoded, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
		if flusher != nil {
			flusher.Flush()
		}
	}

	content := "pong"
	if marker != "" {
		content = content + "|" + marker
	}
	chunk(map[string]any{"role": "assistant", "content": content}, nil)
	chunk(map[string]any{}, "stop")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// writeChatStreamThenDisconnect 发两帧后直接断开连接，模拟"流中途断流"。
// 用 Hijacker 拿到裸连接再关闭，才能制造真正的传输层中断（而不是正常收尾）。
func writeChatStreamThenDisconnect(w http.ResponseWriter, modelName string) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "hijack unsupported"})
		return
	}
	conn, buf, err := hijacker.Hijack()
	if err != nil {
		return
	}
	_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nCache-Control: no-cache\r\n\r\n")
	for i := 0; i < 2; i++ {
		payload, _ := json.Marshal(map[string]any{
			"id":      "chatcmpl-fake",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   modelName,
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": "partial"}, "finish_reason": nil}},
		})
		_, _ = fmt.Fprintf(buf, "data: %s\n\n", payload)
	}
	_ = buf.Flush()
	// 不写 [DONE]，直接断开
	_ = conn.Close()
}
