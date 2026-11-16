package kitutil

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 上游错误体脱敏回归（service/error.go / relaykit/types）：常见密钥形态
// 必须被遮蔽，不能依赖 URL/字段名包裹。
func TestMaskSensitiveInfoCoversCommonKeyShapes(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		secret string
		want   string
	}{
		{"openai sk key", "bad key sk-proj-abcdefghijklmnop", "sk-proj-abcdefghijklmnop", "sk-***"},
		{"xai key", "upstream said xai-abcdefghijklmnop is bad", "xai-abcdefghijklmnop", "xai-***"},
		{"pbr key", "client pbr-a1b2c3d4e5f6 rejected", "pbr-a1b2c3d4e5f6", "pbr-***"},
		{"google key", "AIzaSyA1234567890abcdefg is invalid", "AIzaSyA1234567890abcdefg", "AIza***"},
		{"bearer token", "Authorization: Bearer sk-abcdefghijklmnop", "sk-abcdefghijklmnop", "Bearer ***"},
		{"url query key", "GET https://api.example.com/v1/messages?key=supersecretvalue failed", "supersecretvalue", "key=***"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MaskSensitiveInfo(tc.input)
			require.NotContains(t, got, tc.secret, "secret must be masked")
			require.True(t, strings.Contains(got, tc.want), "masked output %q must contain %q", got, tc.want)
		})
	}
}

// 普通文案不应被过度脱敏（否则错误信息不可诊断）。
func TestMaskSensitiveInfoLeavesPlainTextIntact(t *testing.T) {
	plain := "bad response status code 500: upstream is busy"
	require.Equal(t, plain, MaskSensitiveInfo(plain))
}

// Bearer 令牌可能是带点的 JWT：即使域名规则先跑，也不得泄漏令牌本体。
func TestMaskSensitiveInfoMasksJWTBearer(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	got := MaskSensitiveInfo("Authorization: Bearer " + jwt)
	require.NotContains(t, got, jwt)
	require.Contains(t, got, "Bearer")
}
