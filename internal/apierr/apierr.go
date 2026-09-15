// Package apierr 定义管理 API 的统一错误包络（docs/api-spec-v1.md §3）。
//
//	{ "error": { "code": "<稳定字符串>", "message": "<人读>", "hint": "<可选>" } }
//
// code 是调用方 AI 的分支依据，不得随文案变化；message 只做人读，不参与判定。
package apierr

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 稳定错误码（api-spec §3 表）。
const (
	CodeInvalidRequest       = "invalid_request"
	CodeValidationFailed     = "validation_failed"
	CodeUnauthorized         = "unauthorized"
	CodeForbiddenScope       = "forbidden_scope"
	CodeLaneNotFound         = "lane_not_found"
	CodeChannelNotFound      = "channel_not_found"
	CodeKeyNotFound          = "key_not_found"
	CodeLogNotFound          = "log_not_found"
	CodeConflict             = "conflict"
	CodeNotInitialized       = "not_initialized"
	CodeLaneHasNoMembers     = "lane_has_no_members"
	CodeMemberChannelMissing = "member_channel_missing"
	CodeInvalidMode          = "invalid_mode"
	CodeUpstreamError        = "upstream_error"
	CodeNoAvailableMember    = "no_available_member"
)

type body struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

type envelope struct {
	Error body `json:"error"`
}

// Write 写出错误包络。
func Write(c *gin.Context, status int, code, message, hint string) {
	c.AbortWithStatusJSON(status, envelope{Error: body{Code: code, Message: message, Hint: hint}})
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

// Unprocessable 422。
func Unprocessable(c *gin.Context, code, message string) {
	Write(c, http.StatusUnprocessableEntity, code, message, "")
}
