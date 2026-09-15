package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
	"time"

	"pbr/internal/apierr"
	"pbr/internal/session"
	"pbr/model"

	"github.com/gin-gonic/gin"
)

// 环境变量指定的管理密钥（token-spec §2.4 恢复途径之一）。
//
//	PBR_ADMIN_KEY   单把管理密钥
//	PBR_ADMIN_KEYS  多把管理密钥，逗号分隔（design-v1 §16.7 轮换过渡用）
//
// 任一非空即覆盖口令派生，且任一把均可全量操作、不区分权限。
const (
	PBREnvAdminKey  = "PBR_ADMIN_KEY"
	PBREnvAdminKeys = "PBR_ADMIN_KEYS"
	// PBREnvSessionTTLHours 会话有效期（小时），缺省 7 天。
	PBREnvSessionTTLHours = "PBR_SESSION_TTL_HOURS"
)

// PBRAdminKeysFromEnv 返回环境变量指定的全部管理密钥（去重、去空）。
// PBR_ADMIN_KEY 与 PBR_ADMIN_KEYS 同时存在时合并，任一匹配即通过。
func PBRAdminKeysFromEnv() []string {
	out := make([]string, 0, 2)
	seen := map[string]bool{}
	add := func(raw string) {
		key := strings.TrimSpace(raw)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, key)
	}
	add(os.Getenv(PBREnvAdminKey))
	for _, part := range strings.Split(os.Getenv(PBREnvAdminKeys), ",") {
		add(part)
	}
	return out
}

// PBRAdminKeyFromEnv 兼容旧签名：返回第一把环境变量管理密钥；未设置时为空串。
func PBRAdminKeyFromEnv() string {
	keys := PBRAdminKeysFromEnv()
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

// IsPBRInitialized 网关是否可用于管理面（已设口令，或由环境变量显式指定了管理密钥）。
func IsPBRInitialized() bool {
	if len(PBRAdminKeysFromEnv()) > 0 {
		return true
	}
	return model.IsPBRInitialized()
}

// VerifyPBRAdminKey 校验管理密钥：环境变量优先，其次口令派生。
func VerifyPBRAdminKey(key string) bool {
	if key == "" {
		return false
	}
	envKeys := PBRAdminKeysFromEnv()
	if len(envKeys) > 0 {
		// 常量时间逐个比较，且不做提前返回的中断（仍对全部候选比较）。
		matched := 0
		for _, envKey := range envKeys {
			matched |= subtle.ConstantTimeCompare([]byte(envKey), []byte(key))
		}
		return matched == 1
	}
	return model.VerifyPBRAdminKey(key)
}

// SessionTTL 会话有效期；PBR_SESSION_TTL_HOURS 可覆盖，非法值回落缺省。
func SessionTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv(PBREnvSessionTTLHours))
	if raw == "" {
		return session.DefaultTTL
	}
	hours, err := time.ParseDuration(raw + "h")
	if err != nil || hours <= 0 {
		return session.DefaultTTL
	}
	return hours
}

// sessionSecretMaterial 会话签名所需的凭据材料。
// 环境变量管理密钥场景下，用其哈希作输入（改环境变量即失效）。
func sessionSecretMaterial() string {
	if envKeys := PBRAdminKeysFromEnv(); len(envKeys) > 0 {
		return model.HashAdminKey(envKeys[0])
	}
	return model.PBRAdminKeySha256()
}

// IssueAdminSession 为通过口令校验的调用方签发会话 Cookie。
func IssueAdminSession(c *gin.Context) bool {
	material := sessionSecretMaterial()
	if material == "" {
		return false
	}
	token, err := session.Issue(material, SessionTTL(), time.Now())
	if err != nil {
		return false
	}
	session.SetCookie(c.Writer, token, SessionTTL(), SessionCookieSecure())
	return true
}

// ClearAdminSession 清除会话 Cookie。
func ClearAdminSession(c *gin.Context) {
	session.ClearCookie(c.Writer, SessionCookieSecure())
}

// SessionCookieSecure 会话 Cookie 是否带 Secure：TLS 开启或显式配置时为 true。
func SessionCookieSecure() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TLS_ENABLED")), "true") {
		return true
	}
	return sessionCookieSecureFn()
}

// sessionCookieSecureFn 允许启动代码注入 common.SessionCookieSecure，避免包依赖环。
var sessionCookieSecureFn = func() bool { return false }

// SetSessionCookieSecureProvider 由启动代码注入 common.SessionCookieSecure。
func SetSessionCookieSecureProvider(fn func() bool) {
	if fn != nil {
		sessionCookieSecureFn = fn
	}
}

// HasAdminSession 当前请求是否持有有效会话 Cookie（供会话状态查询复用）。
func HasAdminSession(c *gin.Context) bool {
	return verifySessionCookie(c)
}

// verifySessionCookie 校验请求携带的会话 Cookie。
func verifySessionCookie(c *gin.Context) bool {
	token := session.TokenFromRequest(c.Request)
	material := sessionSecretMaterial()
	if token == "" || material == "" {
		return false
	}
	return session.Verify(token, material, time.Now()) == nil
}

// PBRAuth 管理面鉴权：接受 `Authorization: Bearer <管理密钥>`（AI/脚本）
// 或 HttpOnly 会话 Cookie（浏览器），二者任一通过即可（token-spec §2.3）。
//
// 与模型面严格分开：模型面收客户端密钥（middleware/pbr_client_auth.go）。
func PBRAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsPBRInitialized() {
			apierr.Write(c, http.StatusConflict, apierr.CodeNotInitialized,
				"gateway is not initialized", "POST /api/v1/setup first")
			return
		}
		if verifySessionCookie(c) {
			c.Next()
			return
		}
		key := bearerToken(c)
		if key == "" || !VerifyPBRAdminKey(key) {
			apierr.Write(c, http.StatusUnauthorized, apierr.CodeUnauthorized,
				"missing or invalid bearer token or session", "")
			return
		}
		c.Next()
	}
}

// bearerToken 只从 Authorization 头取 Bearer 令牌（管理面不接受查询串或 X-Api-Key）。
func bearerToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
