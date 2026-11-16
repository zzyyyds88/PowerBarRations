package controller

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/model"
)

// 指标采集的载体映射（design-v1 §8、api-spec §5.1）：字段必须一一对应，
// 且 nil 载体/零耗时/未计费时不得 panic 或产生幽灵样本。
func TestObserveRequestMetricsMapsCarrier(t *testing.T) {
	require.NotPanics(t, func() { observeRequestMetrics(nil) })

	carrier := &model.PBRLogCarrier{
		Lane:          "test-lane",
		RequestModel:  "test-model",
		ChannelName:   "test-channel",
		HTTPStatus:    http.StatusOK,
		TotalMs:       1500,
		TTFTMs:        250,
		ErrorKind:     "soft_transient",
		UpstreamModel: "upstream-model",
		Usage: &model.PBRTokenUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			CacheReadTokens:  3,
			ReasoningTokens:  4,
			EstimatedCost:    0.25,
		},
	}
	assert.NotPanics(t, func() { observeRequestMetrics(carrier) })

	// 未观测耗时（0）与未计费（Usage=nil）也必须是安全输入。
	empty := &model.PBRLogCarrier{HTTPStatus: http.StatusOK}
	assert.NotPanics(t, func() { observeRequestMetrics(empty) })
}

func TestDurationFromMillis(t *testing.T) {
	assert.Equal(t, time.Duration(0), durationFromMillis(0))
	assert.Equal(t, time.Duration(0), durationFromMillis(-5))
	assert.Equal(t, 250*time.Millisecond, durationFromMillis(250))
}
