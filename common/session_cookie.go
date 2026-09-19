package common

import (
	"fmt"
	"os"
	"strings"
)

// InitSessionCookieSettings parses SESSION_COOKIE_SECURE.
//
// 本网关只提供明文 HTTP（token-spec v1 §2.5）：不自动设置 `Secure`；
// 若由外部反向代理终结 TLS，用 SESSION_COOKIE_SECURE=true 显式开启
// 该单一开关即可，无其他配套必填项。
func InitSessionCookieSettings() error {
	secureRaw := strings.TrimSpace(os.Getenv("SESSION_COOKIE_SECURE"))

	SessionCookieSecure = false

	if secureRaw == "" || strings.EqualFold(secureRaw, "false") {
		return nil
	}

	if !strings.EqualFold(secureRaw, "true") {
		return fmt.Errorf("SESSION_COOKIE_SECURE must be true or false")
	}

	SessionCookieSecure = true
	return nil
}
