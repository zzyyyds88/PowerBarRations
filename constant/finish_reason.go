package constant

import "github.com/zzyyyds88/PowerBarRations/relaykit/types"

// Finish reasons moved to types with the conversion kit.
var (
	FinishReasonStop          = types.FinishReasonStop
	FinishReasonToolCalls     = types.FinishReasonToolCalls
	FinishReasonLength        = types.FinishReasonLength
	FinishReasonFunctionCall  = types.FinishReasonFunctionCall
	FinishReasonContentFilter = types.FinishReasonContentFilter
)
