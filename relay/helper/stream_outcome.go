package helper

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
)

// StreamOutcomeError 把一次流式尝试的非正常结束映射成路由失败
// （ADR 0013 第 4 条、routing-spec §4.1/§4.3）。
//
// 分类必须以 StreamStatus.EndReason 为准，不能一律用 EmptyStreamError：
//   - timeout      → 我方 deadline（归 timeout，不原地重试，冷却按 timeout）；
//   - client_gone  → 客户端断开（归 canceled，不冷却、不换人）；
//   - scanner_error→ 读流失败（能识别 deadline/canceled 时按它们分类，否则 bad_response）；
//   - panic/ping_fail → 异常流（bad_response / soft_transient）。
//
// 返回 nil 表示正常结束或无需改判。已提交的流同样返回错误：调用方据此
// 记失败，但**不得重试、不得二次写**（routing-spec §4.3）。
func StreamOutcomeError(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if info == nil {
		return nil
	}
	if status := info.StreamStatus; status != nil {
		switch status.EndReason {
		case relaycommon.StreamEndReasonTimeout:
			return streamTimeoutError()
		case relaycommon.StreamEndReasonClientGone:
			return streamClientGoneError()
		case relaycommon.StreamEndReasonScannerErr:
			cause := status.EndError
			if errors.Is(cause, context.DeadlineExceeded) {
				return streamTimeoutError()
			}
			if errors.Is(cause, context.Canceled) {
				return streamClientGoneError()
			}
			if cause == nil {
				cause = fmt.Errorf("upstream stream read failed")
			}
			return types.NewError(cause, types.ErrorCodeBadResponseBody)
		case relaycommon.StreamEndReasonPanic:
			cause := status.EndError
			if cause == nil {
				cause = fmt.Errorf("upstream stream handler panic")
			}
			return types.NewError(cause, types.ErrorCodeBadResponseBody)
		case relaycommon.StreamEndReasonPingFail:
			cause := status.EndError
			if cause == nil {
				cause = fmt.Errorf("upstream stream keepalive failed")
			}
			return types.NewError(cause, types.ErrorCodeDoRequestFailed, types.ErrOptionWithStatusCode(http.StatusBadGateway))
		}
	}
	// 正常结束（done/eof/handler_stop）但一个内容帧都没写出：上游空响应。
	if StreamFailedWithoutFrames(c, info) {
		return EmptyStreamError()
	}
	return nil
}

// streamTimeoutError 我方 deadline 到期（FirstEvent/流空闲超时）。
func streamTimeoutError() *types.NewAPIError {
	return types.NewError(
		context.DeadlineExceeded,
		types.ErrorCodeChannelResponseTimeExceeded,
		types.ErrOptionWithStatusCode(http.StatusGatewayTimeout),
		types.ErrOptionWithHideErrMsg("upstream stream timed out"),
	)
}

// streamClientGoneError 客户端在流结束前断开。
func streamClientGoneError() *types.NewAPIError {
	return types.NewError(
		context.Canceled,
		types.ErrorCodeDoRequestFailed,
		types.ErrOptionWithStatusCode(499),
		types.ErrOptionWithHideErrMsg("client closed request"),
	)
}
