package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/logger"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	kitutil "github.com/zzyyyds88/PowerBarRations/relaykit/relayconvert/kitutil"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

//// OpenAIErrorWrapper wraps an error into an OpenAIErrorWithStatusCode
//func OpenAIErrorWrapper(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	text := err.Error()
//	lowerText := strings.ToLower(text)
//	if !strings.HasPrefix(lowerText, "get file base64 from url") && !strings.HasPrefix(lowerText, "mime type is not supported") {
//		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
//			common.SysLog(fmt.Sprintf("error: %s", text))
//			text = "请求上游地址失败"
//		}
//	}
//	openAIError := dto.OpenAIError{
//		Message: text,
//		Type:    "new_api_error",
//		Code:    code,
//	}
//	return &dto.OpenAIErrorWithStatusCode{
//		Error:      openAIError,
//		StatusCode: statusCode,
//	}
//}
//
//func OpenAIErrorWrapperLocal(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	openaiErr := OpenAIErrorWrapper(err, code, statusCode)
//	openaiErr.LocalError = true
//	return openaiErr
//}

func RelayErrorHandler(ctx context.Context, resp *http.Response, showBodyWhenFail bool) (newApiErr *types.NewAPIError) {
	newApiErr = types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
	retryAfterSeconds := parseRetryAfterHeader(resp.Header.Get("Retry-After"))
	defer func() {
		if newApiErr != nil {
			newApiErr.RetryAfterSeconds = retryAfterSeconds
		}
	}()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	CloseResponseBodyGracefully(resp)
	var errResponse dto.GeneralErrorResponse
	responseBodyText := string(responseBody)
	// 上游错误体可能回显请求里的 URL（含 ?key=/?api_key=）或密钥明文。
	// 在进入日志与错误之前统一脱敏，避免凭据随日志/错误响应外泄。
	maskedBody := kitutil.MaskSensitiveInfo(responseBodyText)
	responseBodyPreview := common.LocalLogPreview(maskedBody)
	buildErrWithBody := func(message string) error {
		message = kitutil.MaskSensitiveInfo(message)
		if message == "" {
			return fmt.Errorf("bad response status code %d, body: %s", resp.StatusCode, maskedBody)
		}
		return fmt.Errorf("bad response status code %d, message: %s, body: %s", resp.StatusCode, message, maskedBody)
	}

	err = common.Unmarshal(responseBody, &errResponse)
	if err != nil {
		if showBodyWhenFail {
			newApiErr.Err = buildErrWithBody("")
		} else {
			logger.LogError(ctx, fmt.Sprintf("bad response status code %d, body: %s", resp.StatusCode, responseBodyPreview))
			newApiErr.Err = fmt.Errorf("bad response status code %d", resp.StatusCode)
		}
		return
	}

	if common.GetJsonType(errResponse.Error) == "object" {
		// General format error (OpenAI, Anthropic, Gemini, etc.)
		oaiError := errResponse.TryToOpenAIError()
		if oaiError != nil {
			// 上游 message 也可能带凭据，先脱敏再入 Err/RelayError（日志会直接读 Err）。
			oaiError.Message = kitutil.MaskSensitiveInfo(oaiError.Message)
			newApiErr = types.WithOpenAIError(*oaiError, resp.StatusCode)
			if showBodyWhenFail {
				newApiErr.Err = buildErrWithBody(newApiErr.Error())
			}
			return
		}
	}
	message := errResponse.ToMessage()
	if message == "" {
		// The body parsed as JSON but carried no usable error message; log the
		// raw body so the upstream failure remains diagnosable.
		logger.LogError(ctx, fmt.Sprintf("bad response status code %d with empty error message, body: %s", resp.StatusCode, responseBodyPreview))
	}
	message = kitutil.MaskSensitiveInfo(message)
	newApiErr = types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
	if showBodyWhenFail {
		newApiErr.Err = buildErrWithBody(newApiErr.Error())
	}
	return
}

// parseRetryAfterHeader 解析标准 Retry-After（秒数或 HTTP-date），返回建议等待秒数。
// 非法/过期/空值一律返回 0，由选路层回落到自己的冷却配置。
func parseRetryAfterHeader(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds > 0 {
			return seconds
		}
		return 0
	}
	if when, err := http.ParseTime(raw); err == nil {
		wait := time.Until(when)
		if wait <= 0 {
			return 0
		}
		return int(math.Ceil(wait.Seconds()))
	}
	return 0
}

// ResetStatusCode 按渠道级 status_code_mapping 写入对外状态码（MappedStatusCode）。
//
// 只影响客户端看到的状态码，**不改 StatusCode**：错误分类、重试、冷却与熔断
// 仍按原始上游码判定（ADR 0013 / routing-spec §4.1.2）。
//
// 保留码：503 是模型面的**专属契约**（routing-spec §4.2 / ADR 0002），表示"没有可用
// 渠道"，下游 fallback 分类依赖它。若允许把任意上游错误映射成 503，下游会把
// "上游抖动"误判为"路由全挂"，破坏该契约。目标码限定 400–599，2xx 一律拒绝。
func ResetStatusCode(newApiErr *types.NewAPIError, statusCodeMappingStr string) {
	if newApiErr == nil {
		return
	}
	mapping, err := parseStatusCodeMapping(statusCodeMappingStr)
	if err != nil || len(mapping) == 0 {
		return
	}
	if newApiErr.StatusCode == http.StatusOK {
		return
	}
	if mapped, ok := mapping[newApiErr.StatusCode]; ok {
		newApiErr.MappedStatusCode = mapped
	}
}

// ValidateStatusCodeMapping 校验渠道写入口收到的 status_code_mapping。
// 键与值都必须是十进制 HTTP 状态码；目标码限定 400–599，禁止 2xx 与 503。
// 空值与 `{}` 合法（表示不映射/清空）。
func ValidateStatusCodeMapping(raw string) error {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return nil
	}
	if _, err := parseStatusCodeMapping(raw); err != nil {
		return err
	}
	return nil
}

// parseStatusCodeMapping 解析并校验映射表。返回 nil 错误时 map 一定合法。
func parseStatusCodeMapping(raw string) (map[int]int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return nil, nil
	}
	var decoded map[string]any
	if err := common.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, fmt.Errorf("status_code_mapping 必须是 JSON 对象")
	}
	result := make(map[int]int, len(decoded))
	for key, value := range decoded {
		source, err := strconv.Atoi(strings.TrimSpace(key))
		if err != nil || source < 100 || source > 599 {
			return nil, fmt.Errorf("status_code_mapping 的键必须是 100–599 的十进制状态码：%q", key)
		}
		target, ok := parseStatusCodeMappingValue(value)
		if !ok || target < 400 || target > 599 || target == http.StatusServiceUnavailable {
			return nil, fmt.Errorf("status_code_mapping 的目标码必须是 400–599 且不能是 503：%q -> %v", key, value)
		}
		result[source] = target
	}
	return result, nil
}

func parseStatusCodeMappingValue(value any) (int, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return 0, false
		}
		statusCode, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return statusCode, true
	case float64:
		if v != math.Trunc(v) {
			return 0, false
		}
		return int(v), true
	case int:
		return v, true
	case json.Number:
		statusCode, err := strconv.Atoi(v.String())
		if err != nil {
			return 0, false
		}
		return statusCode, true
	default:
		return 0, false
	}
}
