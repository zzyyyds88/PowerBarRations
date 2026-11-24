package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	kitutil "github.com/zzyyyds88/PowerBarRations/relaykit/relayconvert/kitutil"
)

type OpenAIError struct {
	Message  string          `json:"message"`
	Type     string          `json:"type"`
	Param    string          `json:"param"`
	Code     any             `json:"code"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

type ClaudeError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

type ErrorType string

const (
	ErrorTypeNewAPIError   ErrorType = "new_api_error"
	ErrorTypeOpenAIError   ErrorType = "openai_error"
	ErrorTypeClaudeError   ErrorType = "claude_error"
	ErrorTypeGeminiError   ErrorType = "gemini_error"
	ErrorTypeRerankError   ErrorType = "rerank_error"
	ErrorTypeUpstreamError ErrorType = "upstream_error"
)

type ErrorCode string

const (
	ErrorCodeInvalidRequest         ErrorCode = "invalid_request"
	ErrorCodeSensitiveWordsDetected ErrorCode = "sensitive_words_detected"

	// new api error
	ErrorCodeCountTokenFailed   ErrorCode = "count_token_failed"
	ErrorCodeModelPriceError    ErrorCode = "model_price_error"
	ErrorCodeInvalidApiType     ErrorCode = "invalid_api_type"
	ErrorCodeJsonMarshalFailed  ErrorCode = "json_marshal_failed"
	ErrorCodeDoRequestFailed    ErrorCode = "do_request_failed"
	ErrorCodeGetChannelFailed   ErrorCode = "get_channel_failed"
	ErrorCodeGenRelayInfoFailed ErrorCode = "gen_relay_info_failed"

	// channel error
	ErrorCodeChannelNoAvailableKey        ErrorCode = "channel:no_available_key"
	ErrorCodeChannelParamOverrideInvalid  ErrorCode = "channel:param_override_invalid"
	ErrorCodeChannelHeaderOverrideInvalid ErrorCode = "channel:header_override_invalid"
	ErrorCodeChannelModelMappedError      ErrorCode = "channel:model_mapped_error"
	ErrorCodeChannelAwsClientError        ErrorCode = "channel:aws_client_error"
	ErrorCodeChannelInvalidKey            ErrorCode = "channel:invalid_key"
	ErrorCodeChannelResponseTimeExceeded  ErrorCode = "channel:response_time_exceeded"

	// client request error
	ErrorCodeReadRequestBodyFailed ErrorCode = "read_request_body_failed"
	ErrorCodeConvertRequestFailed  ErrorCode = "convert_request_failed"
	ErrorCodeAccessDenied          ErrorCode = "access_denied"

	// request error
	ErrorCodeBadRequestBody ErrorCode = "bad_request_body"

	// response error
	ErrorCodeReadResponseBodyFailed ErrorCode = "read_response_body_failed"
	ErrorCodeBadResponseStatusCode  ErrorCode = "bad_response_status_code"
	ErrorCodeBadResponse            ErrorCode = "bad_response"
	ErrorCodeBadResponseBody        ErrorCode = "bad_response_body"
	ErrorCodeEmptyResponse          ErrorCode = "empty_response"
	ErrorCodeAwsInvokeError         ErrorCode = "aws_invoke_error"
	ErrorCodeModelNotFound          ErrorCode = "model_not_found"
	ErrorCodePromptBlocked          ErrorCode = "prompt_blocked"
	// 隐私脱敏（privacy-spec §6.3）：block 动作拦截，或脱敏引擎失败而未发送。
	ErrorCodePrivacyBlocked          ErrorCode = "privacy_blocked"
	ErrorCodePrivacyUnavailable      ErrorCode = "privacy_unavailable"
	ErrorCodePrivacyModelUnavailable ErrorCode = "privacy_model_unavailable"
	// ErrorCodeUpstreamReject 上游对该成员的拒绝（routing-spec §4.4）：
	// 在响应流内检测到 error 帧（HTTP 200 之后），且尚未向客户端提交任何数据帧。
	// 归类为成员级拒绝（换成员、不冷却、不计熔断分），不是传输层失败。
	ErrorCodeUpstreamReject ErrorCode = "upstream_reject"

	// sql error
	ErrorCodeQueryDataError  ErrorCode = "query_data_error"
	ErrorCodeUpdateDataError ErrorCode = "update_data_error"

	// quota error
	ErrorCodeInsufficientUserQuota      ErrorCode = "insufficient_user_quota"
	ErrorCodePreConsumeTokenQuotaFailed ErrorCode = "pre_consume_token_quota_failed"
)

type NewAPIError struct {
	Err        error
	RelayError any
	skipRetry  bool
	// displayMessage 客户端可见的展示文案（可脱敏/可替换上游原文）。
	// 它只影响 ToOpenAIError/ToClaudeError/MaskSensitiveError 的输出，
	// 不替换 Err——错误链必须保留，否则 errors.Is(DeadlineExceeded/Canceled)
	// 会失效，超时与客户端取消会被误分类（ADR 0013）。
	displayMessage string
	recordErrorLog *bool
	errorType      ErrorType
	errorCode      ErrorCode
	StatusCode     int
	// MappedStatusCode 渠道级 status_code_mapping 解析出的对外状态码（0 = 未映射）。
	// 它只影响写给客户端的响应码，不改 StatusCode——错误分类、重试、冷却与熔断
	// 必须继续按原始上游码判定（ADR 0013 / routing-spec §4.1.2）。
	MappedStatusCode int
	Metadata         json.RawMessage
	// RetryAfterSeconds 上游 Retry-After 头解析出的建议等待秒数（0 = 未提供）。
	// 选路层用它把成员冷却至少延长到上游要求的时间，避免继续撞限流。
	RetryAfterSeconds int
}

// Unwrap enables errors.Is / errors.As to work with NewAPIError by exposing the underlying error.
func (e *NewAPIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *NewAPIError) GetErrorCode() ErrorCode {
	if e == nil {
		return ""
	}
	return e.errorCode
}

func (e *NewAPIError) GetErrorType() ErrorType {
	if e == nil {
		return ""
	}
	return e.errorType
}

// ClientStatusCode 返回写给客户端的 HTTP 状态码。
//
// MappedStatusCode 非零时优先（渠道级 status_code_mapping 的展示映射，ADR 0013）；
// 映射只允许 400–599，因此这里再做一次防御性范围检查，异常值回落到原始状态码。
// 错误分类（internal/route.Classify）始终读 StatusCode，不读本方法。
func (e *NewAPIError) ClientStatusCode() int {
	if e == nil {
		return 0
	}
	if e.MappedStatusCode >= 400 && e.MappedStatusCode <= 599 {
		return e.MappedStatusCode
	}
	return e.StatusCode
}

func (e *NewAPIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		// fallback message when underlying error is missing
		return string(e.errorCode)
	}
	return e.Err.Error()
}

func (e *NewAPIError) ErrorWithStatusCode() string {
	if e == nil {
		return ""
	}
	msg := e.Error()
	if e.StatusCode == 0 {
		return msg
	}
	if msg == "" {
		return fmt.Sprintf("status_code=%d", e.StatusCode)
	}
	return fmt.Sprintf("status_code=%d, %s", e.StatusCode, msg)
}

// MaskSensitiveError 返回可展示/可入库的脱敏文案。
// displayMessage 已由设置方负责脱敏，优先返回；否则对 Err 做通用脱敏。
func (e *NewAPIError) MaskSensitiveError() string {
	if e == nil {
		return ""
	}
	if e.displayMessage != "" {
		return e.displayMessage
	}
	if e.Err == nil {
		return string(e.errorCode)
	}
	errStr := e.Err.Error()
	if e.errorCode == ErrorCodeCountTokenFailed {
		return errStr
	}
	return kitutil.MaskSensitiveInfo(errStr)
}

func (e *NewAPIError) MaskSensitiveErrorWithStatusCode() string {
	if e == nil {
		return ""
	}
	msg := e.MaskSensitiveError()
	if e.StatusCode == 0 {
		return msg
	}
	if msg == "" {
		return fmt.Sprintf("status_code=%d", e.StatusCode)
	}
	return fmt.Sprintf("status_code=%d, %s", e.StatusCode, msg)
}

// SetMessage 设置客户端可见的展示文案。不替换 Err——错误链用于分类与排障（ADR 0013）。
func (e *NewAPIError) SetMessage(message string) {
	if e == nil {
		return
	}
	e.displayMessage = message
}

// DisplayMessage 返回客户端可见文案（未设置时回落 Error()）。
func (e *NewAPIError) DisplayMessage() string {
	if e == nil {
		return ""
	}
	if e.displayMessage != "" {
		return e.displayMessage
	}
	return e.Error()
}

func (e *NewAPIError) ToOpenAIError() OpenAIError {
	var result OpenAIError
	switch e.errorType {
	case ErrorTypeOpenAIError:
		if openAIError, ok := e.RelayError.(OpenAIError); ok {
			result = openAIError
		}
	case ErrorTypeClaudeError:
		if claudeError, ok := e.RelayError.(ClaudeError); ok {
			result = OpenAIError{
				Message: e.Error(),
				Type:    claudeError.Type,
				Param:   "",
				Code:    e.errorCode,
			}
		}
	default:
		result = OpenAIError{
			Message: e.Error(),
			Type:    string(e.errorType),
			Param:   "",
			Code:    e.errorCode,
		}
	}
	if e.displayMessage != "" {
		result.Message = e.displayMessage
	}
	if e.errorCode != ErrorCodeCountTokenFailed {
		result.Message = kitutil.MaskSensitiveInfo(result.Message)
	}
	if result.Message == "" {
		result.Message = string(e.errorType)
	}
	return result
}

func (e *NewAPIError) ToClaudeError() ClaudeError {
	var result ClaudeError
	switch e.errorType {
	case ErrorTypeOpenAIError:
		if openAIError, ok := e.RelayError.(OpenAIError); ok {
			result = ClaudeError{
				Message: e.Error(),
				Type:    fmt.Sprintf("%v", openAIError.Code),
			}
		}
	case ErrorTypeClaudeError:
		if claudeError, ok := e.RelayError.(ClaudeError); ok {
			result = claudeError
		}
	default:
		result = ClaudeError{
			Message: e.Error(),
			Type:    string(e.errorType),
		}
	}
	if e.displayMessage != "" {
		result.Message = e.displayMessage
	}
	if e.errorCode != ErrorCodeCountTokenFailed {
		result.Message = kitutil.MaskSensitiveInfo(result.Message)
	}
	if result.Message == "" {
		result.Message = string(e.errorType)
	}
	return result
}

type NewAPIErrorOptions func(*NewAPIError)

func NewError(err error, errorCode ErrorCode, ops ...NewAPIErrorOptions) *NewAPIError {
	var newErr *NewAPIError
	// 保留深层传递的 new err
	if errors.As(err, &newErr) {
		for _, op := range ops {
			op(newErr)
		}
		return newErr
	}
	e := &NewAPIError{
		Err:        err,
		RelayError: nil,
		errorType:  ErrorTypeNewAPIError,
		StatusCode: http.StatusInternalServerError,
		errorCode:  errorCode,
	}
	for _, op := range ops {
		op(e)
	}
	return e
}

func NewOpenAIError(err error, errorCode ErrorCode, statusCode int, ops ...NewAPIErrorOptions) *NewAPIError {
	var newErr *NewAPIError
	// 保留深层传递的 new err
	if errors.As(err, &newErr) {
		if newErr.RelayError == nil {
			openaiError := OpenAIError{
				Message: newErr.Error(),
				Type:    string(errorCode),
				Code:    errorCode,
			}
			newErr.RelayError = openaiError
		}
		for _, op := range ops {
			op(newErr)
		}
		return newErr
	}
	openaiError := OpenAIError{
		Message: err.Error(),
		Type:    string(errorCode),
		Code:    errorCode,
	}
	e := WithOpenAIError(openaiError, statusCode, ops...)
	// 保留原始 cause：errors.Is(DeadlineExceeded/Canceled) 依赖这条链（ADR 0013）。
	// 展示文案仍走 RelayError.Message，不受影响。
	if err != nil {
		e.Err = err
	}
	return e
}

func InitOpenAIError(errorCode ErrorCode, statusCode int, ops ...NewAPIErrorOptions) *NewAPIError {
	openaiError := OpenAIError{
		Type: string(errorCode),
		Code: errorCode,
	}
	return WithOpenAIError(openaiError, statusCode, ops...)
}

func NewErrorWithStatusCode(err error, errorCode ErrorCode, statusCode int, ops ...NewAPIErrorOptions) *NewAPIError {
	e := &NewAPIError{
		Err: err,
		RelayError: OpenAIError{
			Message: err.Error(),
			Type:    string(errorCode),
		},
		errorType:  ErrorTypeNewAPIError,
		StatusCode: statusCode,
		errorCode:  errorCode,
	}
	for _, op := range ops {
		op(e)
	}

	return e
}

func WithOpenAIError(openAIError OpenAIError, statusCode int, ops ...NewAPIErrorOptions) *NewAPIError {
	code, ok := openAIError.Code.(string)
	if !ok {
		if openAIError.Code != nil {
			code = fmt.Sprintf("%v", openAIError.Code)
		} else {
			code = "unknown_error"
		}
	}
	if openAIError.Type == "" {
		openAIError.Type = "upstream_error"
	}
	e := &NewAPIError{
		RelayError: openAIError,
		errorType:  ErrorTypeOpenAIError,
		StatusCode: statusCode,
		Err:        errors.New(openAIError.Message),
		errorCode:  ErrorCode(code),
	}
	// OpenRouter
	if len(openAIError.Metadata) > 0 {
		openAIError.Message = fmt.Sprintf("%s (%s)", openAIError.Message, openAIError.Metadata)
		e.Metadata = openAIError.Metadata
		e.RelayError = openAIError
		e.Err = errors.New(openAIError.Message)
	}
	for _, op := range ops {
		op(e)
	}
	return e
}

func WithClaudeError(claudeError ClaudeError, statusCode int, ops ...NewAPIErrorOptions) *NewAPIError {
	if claudeError.Type == "" {
		claudeError.Type = "upstream_error"
	}
	e := &NewAPIError{
		RelayError: claudeError,
		errorType:  ErrorTypeClaudeError,
		StatusCode: statusCode,
		Err:        errors.New(claudeError.Message),
		errorCode:  ErrorCode(claudeError.Type),
	}
	for _, op := range ops {
		op(e)
	}
	return e
}

func IsChannelError(err *NewAPIError) bool {
	if err == nil {
		return false
	}
	return strings.HasPrefix(string(err.errorCode), "channel:")
}

func IsSkipRetryError(err *NewAPIError) bool {
	if err == nil {
		return false
	}

	return err.skipRetry
}

func ErrOptionWithSkipRetry() NewAPIErrorOptions {
	return func(e *NewAPIError) {
		e.skipRetry = true
	}
}

func ErrOptionWithStatusCode(statusCode int) NewAPIErrorOptions {
	return func(e *NewAPIError) {
		e.StatusCode = statusCode
	}
}

// ErrOptionWithRetryAfter 记录上游 Retry-After 的建议等待秒数。
func ErrOptionWithRetryAfter(seconds int) NewAPIErrorOptions {
	return func(e *NewAPIError) {
		if seconds > 0 {
			e.RetryAfterSeconds = seconds
		}
	}
}

// ErrOptionWithHideErrMsg 设置客户端可见文案，同时**保留**底层 Err 的错误链。
// 旧实现用 errors.New(replaceStr) 覆盖 e.Err，会销毁 DeadlineExceeded/Canceled 链，
// 导致超时被归为 soft_transient、客户端取消被归为成员故障（ADR 0013）。
func ErrOptionWithHideErrMsg(replaceStr string) NewAPIErrorOptions {
	return func(e *NewAPIError) {
		if kitutil.Debug.Load() {
			fmt.Printf("ErrOptionWithHideErrMsg: %s, origin error: %s", replaceStr, e.Err)
		}
		e.displayMessage = replaceStr
	}
}

func IsRecordErrorLog(e *NewAPIError) bool {
	if e == nil {
		return false
	}
	if e.recordErrorLog == nil {
		// default to true if not set
		return true
	}
	return *e.recordErrorLog
}
