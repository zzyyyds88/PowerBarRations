package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"pbr/internal/apierr"
	"pbr/model"

	"github.com/gin-gonic/gin"
)

// PBREnvAdminKey 显式指定管理密钥的环境变量（token-spec §2.4 恢复途径之一）。
const PBREnvAdminKey = "PBR_ADMIN_KEY"

// PBRAdminKeyFromEnv 返回环境变量指定的管理密钥；未设置时为空串。
func PBRAdminKeyFromEnv() string {
	return strings.TrimSpace(os.Getenv(PBREnvAdminKey))
}

// IsPBRInitialized 网关是否可用于管理面（已设口令，或由环境变量显式指定了管理密钥）。
func IsPBRInitialized() bool {
	if PBRAdminKeyFromEnv() != "" {
		return true
	}
	return model.IsPBRInitialized()
}

// VerifyPBRAdminKey 校验管理密钥。
func VerifyPBRAdminKey(key string) bool {
	if key == "" {
		return false
	}
	if envKey := PBRAdminKeyFromEnv(); envKey != "" {
		return subtle.ConstantTimeCompare([]byte(envKey), []byte(key)) == 1
	}
	return model.VerifyPBRAdminKey(key)
}

// PBRAuth 管理面鉴权：只认 `Authorization: Bearer <管理密钥>`。
//
// 与模型面严格分开：模型面收 Authorization 与 X-Api-Key，管理面只收 Authorization，
// 且校验对象是口令派生的管理密钥，客户端密钥在这里必然不通过（token-spec §4.2）。
func PBRAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsPBRInitialized() {
			apierr.Write(c, http.StatusConflict, apierr.CodeNotInitialized,
				"gateway is not initialized", "POST /api/v1/setup first")
			return
		}
		key := bearerToken(c)
		if key == "" || !VerifyPBRAdminKey(key) {
			apierr.Write(c, http.StatusUnauthorized, apierr.CodeUnauthorized,
				"missing or invalid bearer token", "")
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
