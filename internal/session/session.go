// Package session 实现 PBR 管理面的浏览器会话（token-spec §2.3）。
//
// 背景（人机分离）：控制台的人类用「登录口令」换取一个 HttpOnly 会话 Cookie，
// 浏览器不再保存管理密钥；AI/脚本仍用 Base64(SHA256(口令)) 作 Bearer。
//
// 会话令牌是无状态签名串，**不含口令也不含管理密钥**：
//
//	payload = base64url(admin_key_fingerprint) + "." + base64url(expiry_unix) + "." + base64url(random)
//	token   = payload + "." + base64url(HMAC-SHA256(server_secret, payload))
//
// server_secret = SHA256("pbr-session-v1" + admin_key_sha256)
//
// 用 admin_key_sha256 参与派生，使**改口令后所有旧会话自动失效**（拿到新哈希就验不过），
// 无需额外的会话表或撤销列表。进程重启不失效（只要口令不变），符合"自用网关"定位。
package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// randomToken 生成 n 字节随机串（用于让每次签发的会话令牌唯一）。
func randomToken(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// CookieName 会话 Cookie 名（token-spec §2.5）。
const CookieName = "pbr_session"

// DefaultTTL 默认会话有效期（7 天）；可用 PBR_SESSION_TTL_HOURS 覆盖。
const DefaultTTL = 7 * 24 * time.Hour

// ErrInvalid 表示令牌缺失、格式错误、签名不符或已过期。
var ErrInvalid = errors.New("invalid or expired session")

// fingerprintTopic 把 admin_key_sha256 转成签名密钥的域分隔串。
const fingerprintTopic = "pbr-session-v1"

// deriveSecret 由管理密钥哈希派生会话签名密钥。
func deriveSecret(adminKeySha256 string) []byte {
	sum := sha256.Sum256([]byte(fingerprintTopic + adminKeySha256))
	return sum[:]
}

func b64(raw []byte) string          { return base64.RawURLEncoding.EncodeToString(raw) }
func unb64(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }

// Issue 为当前凭据签发会话令牌。adminKeySha256 为库内哈希，now 注入便于测试。
func Issue(adminKeySha256 string, ttl time.Duration, now time.Time) (string, error) {
	if strings.TrimSpace(adminKeySha256) == "" {
		return "", errors.New("admin credential is not initialized")
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	fingerprint := sha256.Sum256([]byte(adminKeySha256))
	random, err := randomToken(16)
	if err != nil {
		return "", err
	}
	payload := fmt.Sprintf("%s.%s.%s",
		b64(fingerprint[:8]),
		b64([]byte(strconv.FormatInt(now.Add(ttl).Unix(), 10))),
		b64([]byte(random)),
	)
	mac := hmac.New(sha256.New, deriveSecret(adminKeySha256))
	mac.Write([]byte(payload))
	return payload + "." + b64(mac.Sum(nil)), nil
}

// Verify 校验令牌：签名、口令指纹、过期时间三者都必须通过。
func Verify(token, adminKeySha256 string, now time.Time) error {
	token = strings.TrimSpace(token)
	if token == "" || strings.TrimSpace(adminKeySha256) == "" {
		return ErrInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 4 {
		return ErrInvalid
	}
	payload := strings.Join(parts[:3], ".")
	sig, err := unb64(parts[3])
	if err != nil {
		return ErrInvalid
	}
	mac := hmac.New(sha256.New, deriveSecret(adminKeySha256))
	mac.Write([]byte(payload))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return ErrInvalid
	}
	fingerprint := sha256.Sum256([]byte(adminKeySha256))
	gotFingerprint, err := unb64(parts[0])
	if err != nil || !hmac.Equal(gotFingerprint, fingerprint[:8]) {
		return ErrInvalid
	}
	rawExpiry, err := unb64(parts[1])
	if err != nil {
		return ErrInvalid
	}
	expiry, err := strconv.ParseInt(string(rawExpiry), 10, 64)
	if err != nil {
		return ErrInvalid
	}
	if now.Unix() >= expiry {
		return ErrInvalid
	}
	return nil
}

// SetCookie 写入 HttpOnly 会话 Cookie。
func SetCookie(w http.ResponseWriter, token string, ttl time.Duration, secure bool) {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie 让浏览器立即丢弃会话 Cookie。
func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// TokenFromRequest 从 Cookie 里取会话令牌（管理面不接受查询串或头里的会话令牌）。
func TokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie == nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}
