// Package apiresp 是管理面响应信封的唯一实现（docs/api-spec-v1.md §2、§3）。
//
// 契约（design-v1 §5.1）：
//
//	成功 —— 裸资源：单对象即对象本身；列表 {items,next_cursor}；动作类返回语义化最小对象
//	        （如 {"changed":3}、{"deleted":true}）。
//	失败 —— {"error":{code,message,hint,details?}}，且**带真实 HTTP 状态码**。
//
// 不存在"HTTP 200 承载业务失败"的管理端点。
//
// 基座（new-api）遗留的 handler 仍写 {success,message,data}，且失败也用 200。这些响应
// 由 Middleware 按**逐路由响应策略**机械改写为上述契约形态；handler 不需要知道信封。
package apiresp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// maxBuffer 是响应改写缓冲上限。超过即放弃改写、原样直通（宁可保留旧形状，
// 也不把大响应（密钥导出、模型清单）整体驻留内存）。
const maxBuffer = 1 << 20

// 稳定错误码（与 internal/apierr 的常量同名同值；此处复制以免 import 环）。
const (
	CodeInvalidRequest   = "invalid_request"
	CodeValidationFailed = "validation_failed"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeUpstreamError    = "upstream_error"
	CodeInternalError    = "internal_error"
)

// envelope 是基座信封的解析结果。
type envelope struct {
	Success *bool           `json:"success"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Base 是基座信封的公开视图，供策略函数使用。
type Base struct {
	Success bool
	Code    string
	Message string
	Data    json.RawMessage
	Status  int
}

// DataValue 把基座 data 原文解成 any；无 data 时返回 nil。
func (b Base) DataValue() any {
	trimmed := bytes.TrimSpace(b.Data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var out any
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return nil
	}
	return out
}

// SuccessFunc 把基座 success:true 的响应体转成契约成功体。
// 返回 nil 表示 204 空体。
type SuccessFunc func(c *gin.Context, base Base) any

// FailureRule 是一条失败映射规则。只按**显式 code / HTTP 状态**匹配，
// 不解析 message 文案（文案一变就会猜错）。
type FailureRule struct {
	BaseCode  string // 基座显式 code（如 "conflict"），空 = 不看
	Status    int    // 基座 HTTP 状态，0 = 不看
	OutStatus int
	OutCode   string
	Hint      string
}

// Policy 是单个路由的响应策略。
type Policy struct {
	// Success 转写成功体；nil 表示"data 原样作为裸响应"。
	Success SuccessFunc
	// Failures 有序匹配，命中即用其状态码与 code。
	Failures []FailureRule
	// Details 从基座响应提取结构化明细（如 conflict 的 blocked 映射）。
	Details func(base Base) any
	// Passthrough 明确声明"本路由不套信封"（SSE 等流式端点）。
	// 守卫测试要求每个已注册路由要么有成功/失败策略，要么显式 Passthrough。
	Passthrough bool
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Policy{}
)

// normalize 去掉管理面前缀，使 /api/x 与 /api/v1/x 共用一份登记。
func normalize(route string) string {
	for _, prefix := range []string{"/api/v1", "/api"} {
		if route == prefix {
			return "/"
		}
		if strings.HasPrefix(route, prefix+"/") {
			return strings.TrimPrefix(route, prefix)
		}
	}
	return route
}

func key(method, route string) string {
	return strings.ToUpper(method) + " " + normalize(route)
}

// Register 登记某路由的响应策略。route 用 gin 的 `c.FullPath()` 形式（含 :param）。
func Register(method, route string, p Policy) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[key(method, route)] = p
}

// RegisterAll 批量登记同一策略（基座面按路由族登记用）。
func RegisterAll(method string, routes []string, p Policy) {
	for _, route := range routes {
		Register(method, route, p)
	}
}

// Lookup 取某路由的策略；未登记返回 (零值, false)。
func Lookup(method, route string) (Policy, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := registry[key(method, route)]
	return p, ok
}

// RegisteredRoutes 返回已登记的全部 "METHOD route"（供守卫测试）。
func RegisteredRoutes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

// ResetForTest 清空注册表（仅测试用，避免用例间串扰）。
func ResetForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[string]Policy{}
}

// Default 是未显式登记策略的路由所用的兜底策略：成功体 = data 原样裸化，
// 失败按 code/状态码归类。因此"新端点忘了登记"不会漏掉信封，只是拿不到定制成功体。
var Default = Policy{
	Failures: []FailureRule{
		{BaseCode: "conflict", OutStatus: http.StatusConflict, OutCode: CodeConflict},
		{BaseCode: "unauthorized", OutStatus: http.StatusUnauthorized, OutCode: "unauthorized"},
		{BaseCode: "forbidden_scope", OutStatus: http.StatusForbidden, OutCode: "forbidden_scope"},
		{BaseCode: "upstream_error", OutStatus: http.StatusBadGateway, OutCode: CodeUpstreamError},
		{BaseCode: "invalid_request", OutStatus: http.StatusBadRequest, OutCode: CodeInvalidRequest},
		{BaseCode: "validation_failed", OutStatus: http.StatusBadRequest, OutCode: CodeValidationFailed},
		{BaseCode: "not_found", OutStatus: http.StatusNotFound, OutCode: CodeNotFound},
		// 基座把失败也写成 200：无显式 code 时按"业务拒绝"处理（绝大多数是参数/状态校验）。
		{Status: http.StatusOK, OutStatus: http.StatusBadRequest, OutCode: CodeValidationFailed},
		// 基座自己已经给了 4xx/5xx：保留该状态码，不要降级成 500。
		{Status: http.StatusBadRequest, OutStatus: http.StatusBadRequest, OutCode: CodeValidationFailed},
		{Status: http.StatusUnauthorized, OutStatus: http.StatusUnauthorized, OutCode: "unauthorized"},
		{Status: http.StatusForbidden, OutStatus: http.StatusForbidden, OutCode: "forbidden_scope"},
		{Status: http.StatusNotFound, OutStatus: http.StatusNotFound, OutCode: CodeNotFound},
		{Status: http.StatusConflict, OutStatus: http.StatusConflict, OutCode: CodeConflict},
		{Status: http.StatusUnprocessableEntity, OutStatus: http.StatusUnprocessableEntity, OutCode: CodeValidationFailed},
	},
	Details: blockedDetails,
}

// blockedDetails 从基座响应提取车道引用明细（渠道名 → 引用它的车道名）。
func blockedDetails(base Base) any {
	value, ok := base.DataValue().(map[string]any)
	if !ok {
		return nil
	}
	if blocked, ok := value["blocked"]; ok {
		return map[string]any{"blocked": blocked}
	}
	return nil
}

// Middleware 把基座信封改写成契约形态。
//
// 对**所有**管理面路由生效：已登记策略的用其策略，未登记的回落 Default。
// 显式 Passthrough 与超过 maxBuffer 的响应不缓冲、不改写。
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		policy := Default
		if registered, ok := Lookup(c.Request.Method, c.FullPath()); ok {
			if registered.Passthrough {
				c.Next()
				return
			}
			policy = registered
		}
		w := &bufferedWriter{ResponseWriter: c.Writer, policy: policy}
		c.Writer = w
		c.Next()
		w.finish(c)
	}
}

// bufferedWriter 缓冲响应体，在 handler 结束后决定是否改写。
type bufferedWriter struct {
	gin.ResponseWriter
	policy  Policy
	body    bytes.Buffer
	pass    bool
	status  int
	written bool
}

func (w *bufferedWriter) WriteHeader(code int) {
	if w.pass {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	if w.status == 0 {
		w.status = code
	}
}

// WriteHeaderNow 在缓冲期间是空操作：真正的状态码在 finish 时写出。
func (w *bufferedWriter) WriteHeaderNow() {
	if w.pass {
		w.ResponseWriter.WriteHeaderNow()
	}
}

// Flush 在缓冲期间是空操作，避免 handler 提前把未改写的头部刷出去。
func (w *bufferedWriter) Flush() {
	if w.pass {
		w.ResponseWriter.Flush()
	}
}

func (w *bufferedWriter) Write(b []byte) (int, error) {
	w.written = true
	if w.pass {
		return w.ResponseWriter.Write(b)
	}
	if w.body.Len()+len(b) > maxBuffer {
		return w.passthrough(b)
	}
	return w.body.Write(b)
}

func (w *bufferedWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

// Written 反映本缓冲层的写入状态：底层 writer 要到 finish 才会被写。
func (w *bufferedWriter) Written() bool { return w.written }

// Status 返回待写出的状态码，供日志/审计中间件读取。
func (w *bufferedWriter) Status() int {
	if w.status != 0 {
		return w.status
	}
	return w.ResponseWriter.Status()
}

// Size 返回已缓冲/已写出的字节数。
func (w *bufferedWriter) Size() int {
	if w.pass {
		return w.ResponseWriter.Size()
	}
	return w.body.Len()
}

// passthrough 放弃改写：把已缓冲内容与本次内容一起直通。
func (w *bufferedWriter) passthrough(b []byte) (int, error) {
	w.pass = true
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	w.ResponseWriter.WriteHeader(status)
	if w.body.Len() > 0 {
		if _, err := w.ResponseWriter.Write(w.body.Bytes()); err != nil {
			return 0, err
		}
		w.body.Reset()
	}
	return w.ResponseWriter.Write(b)
}

// finish 在 handler 结束后决定最终写出的内容。
func (w *bufferedWriter) finish(c *gin.Context) {
	if w.pass {
		return
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}

	// SSE：不缓冲、不改写。
	if strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		w.ResponseWriter.WriteHeader(status)
		if w.body.Len() > 0 {
			_, _ = w.ResponseWriter.Write(w.body.Bytes())
		}
		return
	}

	body := w.body.Bytes()
	if len(body) == 0 {
		w.ResponseWriter.WriteHeader(status)
		return
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil || env.Success == nil {
		// 不是基座信封（已是裸资源或非 JSON）：原样透传。
		w.ResponseWriter.WriteHeader(status)
		_, _ = w.ResponseWriter.Write(body)
		return
	}

	base := Base{Success: *env.Success, Code: env.Code, Message: env.Message, Data: env.Data, Status: status}
	if base.Success {
		out := base.DataValue()
		if w.policy.Success != nil {
			out = w.policy.Success(c, base)
		}
		if out == nil {
			w.ResponseWriter.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w.ResponseWriter, http.StatusOK, out)
		return
	}

	rule, matched := matchFailure(w.policy.Failures, base)
	if !matched {
		rule = fallbackFailure(base)
	}
	message := base.Message
	if message == "" {
		message = http.StatusText(rule.OutStatus)
	}
	var details any
	if w.policy.Details != nil {
		details = w.policy.Details(base)
	}
	WriteError(w.ResponseWriter, rule.OutStatus, rule.OutCode, message, rule.Hint, details)
}

// fallbackFailure 是没有任何规则命中时的兜底：基座把失败写成 200 的按"业务拒绝"
// （绝大多数是参数/状态校验）→ 400；基座已给 4xx 的保留其状态码；其余按内部错误。
func fallbackFailure(base Base) FailureRule {
	switch {
	case base.Status == http.StatusOK:
		return FailureRule{OutStatus: http.StatusBadRequest, OutCode: CodeValidationFailed}
	case base.Status >= 400 && base.Status < 500:
		return FailureRule{OutStatus: base.Status, OutCode: CodeInvalidRequest}
	default:
		return FailureRule{OutStatus: http.StatusInternalServerError, OutCode: CodeInternalError}
	}
}

func matchFailure(rules []FailureRule, base Base) (FailureRule, bool) {
	for _, rule := range rules {
		if rule.BaseCode != "" && !strings.EqualFold(rule.BaseCode, base.Code) {
			continue
		}
		if rule.Status != 0 && rule.Status != base.Status {
			continue
		}
		return rule, true
	}
	return FailureRule{}, false
}

// WriteJSON 写出成功响应（裸资源）。
func WriteJSON(c *gin.Context, status int, body any) {
	writeJSON(c.Writer, status, body)
}

// WriteError 写出 §3 错误包络。
func WriteError(w gin.ResponseWriter, status int, code, message, hint string, details any) {
	if status == 0 {
		status = http.StatusInternalServerError
	}
	if code == "" {
		code = CodeInternalError
	}
	payload := map[string]any{"code": code, "message": message}
	if hint != "" {
		payload["hint"] = hint
	}
	if details != nil {
		payload["details"] = details
	}
	writeJSON(w, status, map[string]any{"error": payload})
}

// Success 是"成功 + 裸资源"的便捷写法。
func Success(c *gin.Context, body any) { WriteJSON(c, http.StatusOK, body) }

// Failure 是"失败 + 错误包络"的便捷写法。
func Failure(c *gin.Context, status int, code, message string) {
	WriteError(c.Writer, status, code, message, "", nil)
}

// FailureDetails 是带 details 的失败写法。
func FailureDetails(c *gin.Context, status int, code, message string, details any) {
	WriteError(c.Writer, status, code, message, "", details)
}

// FailureHint 是带 hint 的失败写法。
func FailureHint(c *gin.Context, status int, code, message, hint string) {
	WriteError(c.Writer, status, code, message, hint, nil)
}

func writeJSON(w gin.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternalError, err.Error(), "", nil)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}
