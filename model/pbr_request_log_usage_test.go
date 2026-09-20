package model

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWritePBRLogWithUsageReasoningTokens 防回归：上游 usage 的 reasoning_tokens
// （completion_tokens_details.reasoning_tokens）必须落进 request_logs.reasoning_tokens。
// 修复前 PBRTokenUsage 构造漏映射该字段，日志恒为 0，排障被误导（误判思考未开）。
func TestWritePBRLogWithUsageReasoningTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	truncateTables(t)

	// 成功请求 + 思考 token：reasoning_tokens 必须如实落库。
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	carrier := EnsurePBRLogCarrier(c)
	carrier.Lane = "lane-rt"
	carrier.RequestModel = "rt-model"
	carrier.InboundFormat = "openai"
	carrier.Success = true
	carrier.HTTPStatus = http.StatusOK

	WritePBRLogWithUsage(c, &PBRTokenUsage{
		PromptTokens:     31,
		CompletionTokens: 177,
		ReasoningTokens:  141,
		IsStream:         true,
	})

	var row PBRRequestLog
	require.NoError(t, DB.Where("lane_name = ?", "lane-rt").First(&row).Error)
	assert.Equal(t, 141, row.ReasoningTokens, "reasoning_tokens 应映射上游思考 token")
	assert.Equal(t, 31, row.PromptTokens)
	assert.Equal(t, 177, row.CompletionTokens)
	assert.True(t, row.IsStream)
	assert.Equal(t, LogTypeConsume, row.Type, "成功请求记为消耗型")

	// 无思考请求：reasoning_tokens=0（确保不是"恒为某值"的假阳性）。
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	carrier2 := EnsurePBRLogCarrier(c2)
	carrier2.Lane = "lane-rt"
	carrier2.RequestModel = "rt-model"
	carrier2.InboundFormat = "openai"
	carrier2.Success = true
	carrier2.HTTPStatus = http.StatusOK
	WritePBRLogWithUsage(c2, &PBRTokenUsage{PromptTokens: 7, CompletionTokens: 4})

	var row2 PBRRequestLog
	require.NoError(t, DB.Where("prompt_tokens = ?", 7).First(&row2).Error)
	assert.Equal(t, 0, row2.ReasoningTokens, "无思考时 reasoning_tokens=0")
}
