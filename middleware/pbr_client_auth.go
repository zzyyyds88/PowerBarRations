package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"pbr/common"
	"pbr/constant"
	"pbr/internal/apierr"
	"pbr/logger"
	"pbr/model"

	"github.com/gin-gonic/gin"
)

// 模型面鉴权：PBR 客户端密钥（docs/token-spec-v1.md §3.4）。
//
// 校验顺序：取凭据 → 哈希查表 → Enabled → ExpiresAt → IPAllowlist → （车道权限在
// 解析出路由键后于 PBRServe 内判定，因为只有那时才知道请求的目标车道）。
//
// 与管理面严格分开：管理密钥不在客户端密钥表里，因此在这里必然不通过（token-spec §4.2）。
//
// 迁移期兜底：未命中 PBR 客户端密钥时回落到基座原有的令牌鉴权，保证存量 `sk-` 令牌
// 在"未导入"前仍可用；导入后（token-spec §4.1）即走 PBR 路径。W7 删除兜底。

// W7 已删除基座令牌兜底（design-v1 §10.2.1）：模型面只认 PBR 客户端密钥，
// 未命中即 401，不再回落到基座 users/tokens 体系。

// PBRTokenAuth 模型面鉴权。
func PBRTokenAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		credential := modelFaceCredential(c)
		if credential == "" {
			abortPBRModelFace(c, http.StatusUnauthorized, apierr.CodeUnauthorized, "missing model-face credential")
			return
		}
		key, err := model.GetClientKeyByPlain(credential)
		if err != nil || key == nil {
			abortPBRModelFace(c, http.StatusUnauthorized, apierr.CodeUnauthorized, "invalid client key")
			return
		}
		if !key.Enabled {
			abortPBRModelFace(c, http.StatusUnauthorized, apierr.CodeUnauthorized, "client key is disabled")
			return
		}
		if key.ExpiresAt != nil && *key.ExpiresAt > 0 && *key.ExpiresAt < time.Now().Unix() {
			abortPBRModelFace(c, http.StatusUnauthorized, apierr.CodeUnauthorized, "client key expired")
			return
		}
		if !ipAllowed(c.ClientIP(), key.IPAllowlist) {
			abortPBRModelFace(c, http.StatusForbidden, apierr.CodeForbiddenScope, "source ip is not allowlisted")
			return
		}
		SetupContextForPBRClientKey(c, key)

		// 限流与并发上限（token-spec §3.4 第 6 步）：超限 429 + Retry-After。
		if key.RateLimitRPM > 0 || key.MaxConcurrency > 0 {
			limiter := limiterFor(key.Id)
			if wait, ok := limiter.acquire(key.RateLimitRPM, key.MaxConcurrency); !ok {
				c.Header("Retry-After", strconv.Itoa(wait))
				apierr.Write(c, http.StatusTooManyRequests, "rate_limited",
					"client key exceeded its rate limit or concurrency cap", "")
				return
			}
			// c.Next() 返回后再释放并发计数，覆盖整个下游链路
			defer limiter.release()
		}
		c.Next()
	}
}

// keyLimiter 每把客户端密钥的进程内限流状态（重启清空）。
type keyLimiter struct {
	mu       sync.Mutex
	recent   []int64 // 最近一分钟内的请求时间（Unix 秒），用于 RPM 滑动窗口
	inflight int
}

var (
	pbrLimitersMu sync.Mutex
	pbrLimiters   = map[int]*keyLimiter{}
)

func limiterFor(id int) *keyLimiter {
	pbrLimitersMu.Lock()
	defer pbrLimitersMu.Unlock()
	limiter, ok := pbrLimiters[id]
	if !ok {
		limiter = &keyLimiter{}
		pbrLimiters[id] = limiter
	}
	return limiter
}

// DropPBRKeyLimiter 丢弃某把客户端密钥的限流状态。
//
// 删除密钥时调用：条目标识是自增主键，删除后不会复用，不清理就会随
// "建了删、删了再建"无限增长（recent 里还留着时间戳）。删除即重置也符合
// 语义——键没了，限流窗口没有保留意义。
func DropPBRKeyLimiter(id int) {
	pbrLimitersMu.Lock()
	defer pbrLimitersMu.Unlock()
	delete(pbrLimiters, id)
}

// acquire 取一个令牌；失败返回建议的 Retry-After 秒数。
//
// 两个上限相互独立（token-spec §3.1：0 = 不限），所以**先判定、后记账**：
// 只有确实被放行的请求才写入 RPM 窗口与并发计数。此前先记 RPM 再查并发，
// 会让"仅因并发超限被拒"的请求白白吃掉 RPM 配额（recent 多记一条），
// 且该路径提前 return 不释放 inflight，导致并发槽被持续占用。
func (l *keyLimiter) acquire(rpm int, maxConcurrency int) (int, bool) {
	now := time.Now().Unix()
	l.mu.Lock()
	defer l.mu.Unlock()

	if rpm > 0 {
		kept := l.recent[:0]
		for _, ts := range l.recent {
			if now-ts < 60 {
				kept = append(kept, ts)
			}
		}
		l.recent = kept
		// 窗口内已满：最早那条滑出窗口时才有名额，剩余等待秒数据此估算。
		if len(l.recent) >= rpm {
			wait := 60 - (now - l.recent[0])
			if wait < 1 {
				wait = 1
			}
			return int(wait), false
		}
	}
	if maxConcurrency > 0 && l.inflight >= maxConcurrency {
		// 并发槽释放时机不确定，给 1 秒让调用方退避重试。
		return 1, false
	}
	if rpm > 0 {
		l.recent = append(l.recent, now)
	}
	l.inflight++
	return 0, true
}

func (l *keyLimiter) release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inflight > 0 {
		l.inflight--
	}
}

// SetupContextForPBRClientKey 把 PBR 客户端身份写进上下文。
//
// 同时把迁移期的记账锚点（系统用户）挂上，使基座转发管道里的记账代码仍能工作
// （design-v1 §10.2 的逻辑停用阶段：额度恒无限、准入不查余额）。
const contextKeyPBRClient = "pbr_client_key"

// PBRClientKeyFrom 取当前请求的 PBR 客户端密钥（未走 PBR 鉴权时为 nil）。
func PBRClientKeyFrom(c *gin.Context) *model.ClientKey {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(contextKeyPBRClient); ok {
		if key, ok := value.(*model.ClientKey); ok {
			return key
		}
	}
	return nil
}

func SetupContextForPBRClientKey(c *gin.Context, key *model.ClientKey) {
	c.Set(contextKeyPBRClient, key)
	systemUserID, err := model.EnsurePBRSystemUser()
	if err == nil && systemUserID > 0 {
		if userCache, cacheErr := model.GetUserCache(systemUserID); cacheErr == nil {
			userCache.WriteContext(c)
		}
		c.Set("id", systemUserID)
		common.SetContextKey(c, constant.ContextKeyUserId, systemUserID)
	}
	common.SetContextKey(c, constant.ContextKeyTokenUnlimited, true)
	c.Set("token_name", key.Name)
	common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
	if common.GetContextKeyString(c, constant.ContextKeyUserGroup) == "" {
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	}
	common.SetContextKey(c, constant.ContextKeyPBRKeyId, key.Id)
	common.SetContextKey(c, constant.ContextKeyPBRKeyName, key.Name)
	// 载体在此建立（早于路由阶段），使 403/503 这类在中间件就结束的请求也有归属。
	carrier := model.EnsurePBRLogCarrier(c)
	carrier.KeyId = key.Id
	carrier.KeyName = key.Name

	go func(id int) {
		defer func() { _ = recover() }()
		model.TouchClientKey(id)
	}(key.Id)
}

// PBRClientKeyName 当前请求所用 PBR 客户端密钥名（未走 PBR 时为空）。
func PBRClientKeyName(c *gin.Context) string {
	return common.GetContextKeyString(c, constant.ContextKeyPBRKeyName)
}

// modelFaceCredential 从模型面的三个兼容位置取凭据（token-spec §3.4 第 1 步）。
func modelFaceCredential(c *gin.Context) string {
	if auth := c.GetHeader("Authorization"); auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}
	if key := strings.TrimSpace(c.GetHeader("X-Api-Key")); key != "" {
		return key
	}
	if key := strings.TrimSpace(c.GetHeader("x-goog-api-key")); key != "" {
		return key
	}
	return strings.TrimSpace(c.Query("key"))
}

func abortPBRModelFace(c *gin.Context, status int, code, message string) {
	logger.LogWarn(c, "pbr client auth rejected: "+message)
	apierr.Write(c, status, code, message, "GET /api/v1/keys")
}

// ipAllowed 空名单 = 不限制（局域网部署通常留空）。
func ipAllowed(clientIP, allowlist string) bool {
	allowlist = strings.TrimSpace(allowlist)
	if allowlist == "" || allowlist == "[]" {
		return true
	}
	var entries []string
	if err := common.UnmarshalJsonStr(allowlist, &entries); err != nil {
		// 名单损坏时拒绝放行（fail-closed）：静默放行等于安全阀失效
		return false
	}
	if len(entries) == 0 {
		return true
	}
	ip := net.ParseIP(clientIP)
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if entry == clientIP {
			return true
		}
		if _, cidr, err := net.ParseCIDR(entry); err == nil && ip != nil && cidr.Contains(ip) {
			return true
		}
	}
	return false
}
