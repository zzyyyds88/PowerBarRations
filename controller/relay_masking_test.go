package controller

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// 客户端可见文案与日志摘要都可能包含上游回显的凭据；必须先脱敏再截断。
func TestMaskedClientMessageRedactsSecrets(t *testing.T) {
	secret := "sk-" + "live-0123456789abcdef"
	err := types.NewOpenAIError(
		errors.New("upstream rejected key="+secret+" at https://private.example.internal/v1"),
		types.ErrorCode(""), 502,
	)
	msg := maskedClientMessage(err, "req-123")
	assert.NotContains(t, msg, secret, "客户端文案不得回显 key")
	assert.Contains(t, msg, "request id: req-123")
}

func TestMaskedErrorSummaryTruncatesAtUTF8Boundary(t *testing.T) {
	raw := strings.Repeat("中文", 2048) // 12288 字节，远超 2048 上限
	got := maskedErrorSummary(raw)
	assert.LessOrEqual(t, len(got), 2048)
	assert.True(t, utf8.ValidString(got), "截断必须落在完整 UTF-8 字符边界")
	assert.NotEmpty(t, got)
}

func TestMaskedErrorSummaryRedactsKeyAndKeepsPlainText(t *testing.T) {
	secret := "sk-" + "proj-abcdef0123456789"
	got := maskedErrorSummary("upstream said invalid key " + secret)
	assert.NotContains(t, got, secret)
	require.NotEmpty(t, got)
}
