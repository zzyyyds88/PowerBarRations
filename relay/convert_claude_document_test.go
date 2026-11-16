package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
	"github.com/zzyyyds88/PowerBarRations/service"
)

// Claude /v1/messages 的文档块转 OpenAI Chat：
// base64 PDF → Chat file part；file_id 引用 → 省略并落
// admin_info.conversion_diagnostics（生产里 Anthropic 客户端带文档走
// OpenAI 兼容渠道时，既不能整请求 400，也不能静默丢掉文档）。
func TestClaudeDocumentConversionRecordsDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "lane",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-4o",
		},
	}

	maxTokens := uint(64)
	req := &dto.ClaudeRequest{
		Model:     "lane",
		MaxTokens: &maxTokens,
		Messages: []dto.ClaudeMessage{{
			Role: "user",
			Content: []any{
				map[string]any{
					"type":   "document",
					"source": &dto.ClaudeMessageSource{Type: "base64", MediaType: "application/pdf", Data: "JVBERi0="},
				},
				map[string]any{
					"type":   "document",
					"source": &dto.ClaudeMessageSource{Type: "file"},
				},
			},
		}},
	}

	result, convErr := service.ConvertRequest(c, info, types.RelayFormatOpenAI, req)
	require.NoError(t, convErr)
	require.NotNil(t, result)

	out, ok := result.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok, "expected OpenAI chat request, got %T", result.Value)
	require.Len(t, out.Messages, 1)
	parts := out.Messages[0].ParseContent()
	require.Len(t, parts, 1)
	assert.Equal(t, dto.ContentTypeFile, parts[0].Type)
	file, ok := parts[0].File.(*dto.MessageFile)
	require.True(t, ok, "expected file part, got %T", parts[0].File)
	assert.Equal(t, "data:application/pdf;base64,JVBERi0=", file.FileData)

	diagnostics := info.ConversionDiagnostics()
	require.True(t, hasHostDiagnosticCode(diagnostics, "unsupported_media_source"))

	other := service.GenerateTextOtherInfo(c, info, 0)
	adminInfo, ok := other.Snapshot()["admin_info"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, adminInfo, "conversion_diagnostics")
}
