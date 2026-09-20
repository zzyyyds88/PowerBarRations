// Package apierr 定义管理 API 的统一错误包络（docs/api-spec-v1.md §3）。
//
//	{ "error": { "code": "<稳定字符串>", "message": "<人读>", "hint": "<可选>", "details": <可选> } }
//
// code 是调用方 AI 的分支依据，不得随文案变化；message 只做人读，不参与判定。
// 失败**必须带真实 HTTP 状态码**——不存在"HTTP 200 承载业务失败"的管理端点。
package apierr

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 稳定错误码（api-spec §3 表）。
const (
	CodeInvalidRequest        = "invalid_request"
	CodeValidationFailed      = "validation_failed"
	// CodeDryRunNotSupported：该端点声明 dry-run 为 reject，带 ?dry_run=true 时
	// 必须返回 400 且**不执行任何写操作**（api-spec §2.4）。
	CodeDryRunNotSupported = "dry_run_not_supported"
	CodeUnauthorized          = "unauthorized"
	CodeForbiddenScope        = "forbidden_scope"
	CodeLaneNotFound          = "lane_not_found"
	CodeChannelNotFound       = "channel_not_found"
	CodeKeyNotFound           = "key_not_found"
	CodeLogNotFound           = "log_not_found"
	CodeModelNotFound         = "model_not_found"
	CodeTaskNotFound          = "task_not_found"
	CodePrefillGroupNotFound  = "prefill_group_not_found"
	CodeConflict              = "conflict"
	CodeNotInitialized        = "not_initialized"
	CodeLaneHasNoMembers      = "lane_has_no_members"
	CodeMemberChannelMissing  = "member_channel_missing"
	CodeInvalidMode           = "invalid_mode"
	CodeUpstreamError         = "upstream_error"
	CodeNoAvailableMember     = "no_available_member"
	CodeWebhookTargetNotFound = "webhook_target_not_found"
	CodeInternalError         = "internal_error"
)

type body struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Details any    `json:"details,omitempty"`
}

type envelope struct {
	Error body `json:"error"`
}

// Write 写出错误包络。
func Write(c *gin.Context, status int, code, message, hint string) {
	WriteDetails(c, status, code, message, hint, nil)
}

// WriteDetails 写出带结构化明细的错误包络。
func WriteDetails(c *gin.Context, status int, code, message, hint string, details any) {
	c.AbortWithStatusJSON(status, envelope{Error: body{Code: code, Message: message, Hint: hint, Details: details}})
}

// BadRequest 400 invalid_request。
func BadRequest(c *gin.Context, message string) {
	Write(c, http.StatusBadRequest, CodeInvalidRequest, message, "")
}

// Validation 400 validation_failed。
func Validation(c *gin.Context, message string) {
	Write(c, http.StatusBadRequest, CodeValidationFailed, message, "")
}

// NotFound 404。
func NotFound(c *gin.Context, code, message, hint string) {
	Write(c, http.StatusNotFound, code, message, hint)
}

// Conflict 409。
func Conflict(c *gin.Context, code, message, hint string) {
	Write(c, http.StatusConflict, code, message, hint)
}

// ConflictDetails 409，携带结构化明细（如车道引用守卫的 blocked 映射）。
func ConflictDetails(c *gin.Context, code, message, hint string, details any) {
	WriteDetails(c, http.StatusConflict, code, message, hint, details)
}

// Unprocessable 422。
func Unprocessable(c *gin.Context, code, message string) {
	Write(c, http.StatusUnprocessableEntity, code, message, "")
}

// Internal 500 internal_error。
func Internal(c *gin.Context, message string) {
	Write(c, http.StatusInternalServerError, CodeInternalError, message, "")
}

// UpstreamError 502 upstream_error（上游返回错误）。
func UpstreamError(c *gin.Context, message string) {
	Write(c, http.StatusBadGateway, CodeUpstreamError, message, "")
}

// NotFoundChannel 404 channel_not_found。
func NotFoundChannel(c *gin.Context, name string) {
	NotFound(c, CodeChannelNotFound, "channel '"+name+"' not found", "GET /api/channels")
}

// NotFoundModel 404 model_not_found。
func NotFoundModel(c *gin.Context, name string) {
	NotFound(c, CodeModelNotFound, "model '"+name+"' not found", "GET /api/model-metadata")
}

// NotFoundTask 404 task_not_found。
func NotFoundTask(c *gin.Context, id string) {
	NotFound(c, CodeTaskNotFound, "task '"+id+"' not found", "GET /api/system-tasks")
}

// NotFoundPrefillGroup 404 prefill_group_not_found。
func NotFoundPrefillGroup(c *gin.Context, id string) {
	NotFound(c, CodePrefillGroupNotFound, "prefill group '"+id+"' not found", "GET /api/prefill-groups")
}
