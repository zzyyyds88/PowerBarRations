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

// 环境变量指定的管理密钥（token-spec §2.4 恢复途径之一）。
//
//	PBR_ADMIN_KEY   单把管理密钥
//	PBR_ADMIN_KEYS  多把管理密钥，逗号分隔（design-v1 §16.7 轮换过渡用）
//
// 任一非空即覆盖口令派生，且任一把均可全量操作、不区分权限。
const (
	PBREnvAdminKey  = "PBR_ADMIN_KEY"
	PBREnvAdminKeys = "PBR_ADMIN_KEYS"
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
