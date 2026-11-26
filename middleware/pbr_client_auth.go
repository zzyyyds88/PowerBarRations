package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	pbrroute "github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/logger"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// 模型面鉴权：PBR 客户端密钥（docs/token-spec-v1.md §3.4）。
//
// 校验顺序：取凭据 → 哈希查表 → Enabled → ExpiresAt → IPAllowlist → （车道权限在
// 解析出路由键后于 PBRServe 内判定，因为只有那时才知道请求的目标车道）。
//
// 与管理面严格分开：管理密钥不在客户端密钥表里，因此在这里必然不通过（token-spec §4.2）。
//
// 兜底历史：设计上曾有"未命中 PBR 客户端密钥时回落基座令牌鉴权"的迁移期兜底，
// 已随 W7 删除（design-v1 §10.2.1）——模型面只认 PBR 客户端密钥，未命中即 401。

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
			lease, wait, ok := limiter.acquire(key.RateLimitRPM, key.MaxConcurrency)
			if !ok {
				c.Header("Retry-After", strconv.Itoa(wait))
				writeModelFaceRejectionLog(c, http.StatusTooManyRequests,
					"client key exceeded its rate limit or concurrency cap", string(pbrroute.KindLocalThrottle))
				apierr.Write(c, http.StatusTooManyRequests, "rate_limited",
					"client key exceeded its rate limit or concurrency cap", "")
				return
			}
			c.Set(contextKeyPBRClientLease, lease)
			// 中间件出口兜底；PBRServe 创建 State 后会把同一个幂等 lease.Release
			// 注册为 State cleanup，两者重复调用不会重复归还并发槽。
			defer lease.Release()
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

// KeyLease 是一次 acquire 对应的不可重复释放凭证。Release 幂等，且只能释放
// 创建它的 limiter；重复释放不会影响同一 key 的另一个在飞请求。
type KeyLease struct {
	limiter *keyLimiter
	once    sync.Once
}

func (l *KeyLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		if l.limiter != nil {
			l.limiter.release(l)
		}
	})
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
	delete(pbrLimiters, id)
	pbrLimitersMu.Unlock()
	model.DropClientKeyTouchState(id)
}

// acquire 取一个令牌；失败返回建议的 Retry-After 秒数。
//
// 两个上限相互独立（token-spec §3.1：0 = 不限），所以**先判定、后记账**：
// 只有确实被放行的请求才写入 RPM 窗口与并发计数。
func (l *keyLimiter) acquire(rpm int, maxConcurrency int) (*KeyLease, int, bool) {
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
		if len(l.recent) >= rpm {
			// 动态降低 RPM 后，recent[0] 可能早已超出新上限；必须等待第
			// len-rpm 条滑出，窗口内才会重新低于 rpm。
			wait := 60 - (now - l.recent[len(l.recent)-rpm])
			if wait < 1 {
				wait = 1
			}
			return nil, int(wait), false
		}
	}
	if maxConcurrency > 0 && l.inflight >= maxConcurrency {
		return nil, 1, false
	}
	if rpm > 0 {
		l.recent = append(l.recent, now)
	}
	l.inflight++
	return &KeyLease{limiter: l}, 0, true
}

func (l *keyLimiter) release(lease *KeyLease) {
	if l == nil || lease == nil || lease.limiter != l {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inflight > 0 {
		l.inflight--
	}
}

// SetupContextForPBRClientKey 把 PBR 客户端身份写进上下文。
//
// 同时把迁移期的记账锚点（系统用户）挂上，使基座转发管道里的记账代码仍能工作
// （design-v1 §10.2 的逻辑停用阶段：锚点用户无余额语义——quota 列已按 §3.4 删除，
// 准入不查余额）。
const (
	contextKeyPBRClient      = "pbr_client_key"
	contextKeyPBRClientLease = "pbr_client_key_lease"
)

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

// PBRClientKeyLeaseFrom 取当前请求持有的一次性客户端密钥 lease。PBRServe
// 创建 State 后应通过 State.AddCleanup(lease.Release) 注册同一凭证。
func PBRClientKeyLeaseFrom(c *gin.Context) *KeyLease {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(contextKeyPBRClientLease); ok {
		if lease, ok := value.(*KeyLease); ok {
			return lease
		}
	}
	return nil
}

func SetupContextForPBRClientKey(c *gin.Context, key *model.ClientKey) {
	c.Set(contextKeyPBRClient, key)
	if base, err := model.PBRSystemUserBase(); err == nil && base != nil {
		base.WriteContext(c)
		c.Set("id", base.Id)
		common.SetContextKey(c, constant.ContextKeyUserId, base.Id)
	} else {
		if err != nil {
			common.SysError("pbr: system user base unavailable: " + err.Error())
		} else {
			common.SysError("pbr: system user base unavailable")
		}
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

	// TouchClientKey 只做有界 map 合并与非阻塞通知，不在请求路径起 goroutine。
	model.TouchClientKey(key.Id)
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
	// 查询串 `?key=` 不再作为凭据来源（token-spec §3.4 只承诺 Authorization /
	// X-Api-Key；x-goog-api-key 为 Gemini 兼容保留）。凭据进 query 会随访问日志、
	// Referer、浏览器历史与代理日志泄漏。
	return ""
}

func abortPBRModelFace(c *gin.Context, status int, code, message string) {
	logger.LogWarn(c, "pbr client auth rejected: "+message)
	// 鉴权失败也要留日志行：否则"谁在拿错 key 撞模型面"在管理面完全不可见
	// （此前只有进程日志，request_logs 查不到）。
	writeModelFaceRejectionLog(c, status, message, string(pbrroute.KindClientError))
	apierr.Write(c, status, code, message, "GET /api/v1/keys")
}

// writeModelFaceRejectionLog 为在中间件阶段就被拒绝的模型面请求补一条日志。
// 此时没有选路/上游信息，只记录可达字段（IP、已识别的密钥、状态与原因）。
func writeModelFaceRejectionLog(c *gin.Context, status int, message, errorKind string) {
	carrier := model.EnsurePBRLogCarrier(c)
	if carrier == nil {
		return
	}
	carrier.Success = false
	carrier.HTTPStatus = status
	carrier.ErrorKind = errorKind
	carrier.ErrorSummary = message
	carrier.Ip = c.ClientIP()
	model.WritePBRLog(c)
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
