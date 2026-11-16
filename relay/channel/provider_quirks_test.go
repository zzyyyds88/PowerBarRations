package channel

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 服务商硬性要求：网关按目标 host 自动补齐，不依赖用户 param_override。
func TestProviderQuirksOpenCodeSessionHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		target     string
		client     string
		wantValue  string
		wantHeader bool
	}{
		{
			name:       "opencode go: fallback when client did not send",
			target:     "https://opencode.ai/zen/go/v1/chat/completions",
			wantValue:  "pbr-gateway",
			wantHeader: true,
		},
		{
			name:       "opencode zen: fallback when client did not send",
			target:     "https://opencode.ai/zen/v1/chat/completions",
			wantValue:  "pbr-gateway",
			wantHeader: true,
		},
		{
			name:       "opencode: client value passes through",
			target:     "https://opencode.ai/zen/go/v1/chat/completions",
			client:     "conversation-123",
			wantValue:  "conversation-123",
			wantHeader: true,
		},
		{
			name:       "unrelated host untouched",
			target:     "https://api.example.com/v1/chat/completions",
			wantHeader: false,
		},
		{
			name:       "opencode host but unrelated path untouched",
			target:     "https://opencode.ai/other/v1/chat/completions",
			wantHeader: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, err := url.Parse(test.target)
			require.NoError(t, err)

			headers := http.Header{}
			c, _ := gin.CreateTestContext(nil)
			c.Request = &http.Request{Header: http.Header{}}
			if test.client != "" {
				c.Request.Header.Set("x-opencode-session", test.client)
			}

			applyProviderQuirksToHeader(target, headers, c)

			if !test.wantHeader {
				assert.Empty(t, headers.Values("x-opencode-session"))
				return
			}
			assert.Equal(t, test.wantValue, headers.Get("x-opencode-session"))
		})
	}
}

// 用户显式配置的 header_override 后应用，必须赢过网关的兜底值。
func TestProviderQuirksDoNotOverrideExplicitHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	target, err := url.Parse("https://opencode.ai/zen/go/v1/chat/completions")
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set("x-opencode-session", "explicit-value")
	c, _ := gin.CreateTestContext(nil)
	c.Request = &http.Request{Header: http.Header{}}

	applyProviderQuirksToHeader(target, headers, c)
	assert.Equal(t, "explicit-value", headers.Get("x-opencode-session"))
}

// OpenCode Go 与 Zen 同 host 不同前缀，不能互相命错。
func TestMatchProviderQuirkDistinguishesSameHost(t *testing.T) {
	goURL, err := url.Parse("https://opencode.ai/zen/go/v1/chat/completions")
	require.NoError(t, err)
	zenURL, err := url.Parse("https://opencode.ai/zen/v1/chat/completions")
	require.NoError(t, err)

	assert.Equal(t, "opencode-go", matchProviderQuirk(goURL).ID)
	assert.Equal(t, "opencode-zen", matchProviderQuirk(zenURL).ID)
	assert.Nil(t, matchProviderQuirk(nil))
}
