package relay

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	kitreasoning "github.com/zzyyyds88/PowerBarRations/relaykit/relayconvert/reasoning"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

func newConvertRequestFailedError(c *gin.Context, info *relaycommon.RelayInfo, err error) *types.NewAPIError {
	var loss *types.ConversionLossError
	if errors.As(err, &loss) {
		info.RecordConversionDiagnostics(c, loss.Diagnostics)
		return types.NewErrorWithStatusCode(err, types.ErrorCodeConvertRequestFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if kitreasoning.IsClientError(err) {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeConvertRequestFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
}
