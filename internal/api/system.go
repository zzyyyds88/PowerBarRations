package api

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"pbr/common"
	"pbr/internal/apierr"
	"pbr/middleware"
	"pbr/model"

	"github.com/gin-gonic/gin"
)

var processStart = time.Now()

// Health GET /api/v1/health（免鉴权）。
func Health(c *gin.Context) {
	dbStatus := "ok"
	if err := model.PingDB(); err != nil {
		dbStatus = "error"
	}
	status := "ok"
	if dbStatus != "ok" {
		status = "degraded"
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   status,
		"version":  common.Version,
		"uptime_s": int64(time.Since(processStart).Seconds()),
		"db":       dbStatus,
	})
}

// Version GET /api/v1/version（免鉴权）。
//
// ui-spec §6.9 的"信息"分节要求展示版本、构建时间、运行时长与数据库路径。
// 数据库路径只回显 DSN 的文件名部分（去掉 ?参数），且不含任何凭据。
func Version(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version":        common.Version,
		"api":            "v1",
		"build_time":     common.BuildTime,
		"uptime_seconds": int64(time.Since(processStart).Seconds()),
		"db_path":        databasePathForDisplay(),
	})
}

// databasePathForDisplay 数据库 DSN 的可展示形式：去掉查询参数（pragma 等）。
func databasePathForDisplay() string {
	dsn := common.SQLitePath
	if idx := strings.IndexByte(dsn, '?'); idx >= 0 {
		dsn = dsn[:idx]
	}
	return dsn
}

// SetupStatus GET /api/v1/setup/status（免鉴权）。
func SetupStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"initialized": middleware.IsPBRInitialized()})
}

type setupRequest struct {
	Password string `json:"password"`
}

// Setup POST /api/v1/setup（免鉴权，仅未初始化时可用）。
func Setup(c *gin.Context) {
	if middleware.IsPBRInitialized() {
		apierr.Conflict(c, apierr.CodeConflict, "already initialized", "POST /api/v1/auth/login")
		return
	}
	var req setupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	if req.Password == "" {
		apierr.Validation(c, "password is required")
		return
	}
	adminKey, err := model.SetPBRAdminPassword(req.Password)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	// PBR 是单用户自用网关：首启即打开自用模式（额度恒无限、准入不查余额），
	// 使未配置单价的模型也能直接用——否则新装的网关必须先配一张单价表才能转发。
	// design-v1 §10.2 的"计费逻辑停用"以此落地；之后可随时经选项面板改回。
	_ = model.UpdateOption("SelfUseModeEnabled", "true")
	// 注意：这里**不能**提前建迁移期记账锚点用户（EnsurePBRSystemUser）——
	// 基座的 /api/setup 在"已存在 root 用户"时会拒绝初始化，提前建会把它挤掉。
	// 锚点改为首次使用 PBR 客户端密钥访问模型面时懒建（SetupContextForPBRClientKey）。
	writeAudit(c, "setup", "admin_credential", "admin")
	// 首启后直接签发浏览器会话：控制台不再需要把管理密钥存 localStorage。
	middleware.IssueAdminSession(c)
	resp := gin.H{
		"initialized": true,
		"admin_key":   adminKey,
		"algorithm":   "base64(sha256(password))",
		"hint":        "登录口令变更后该密钥随之变化",
	}
	if len(req.Password) < recommendedPasswordLen {
		resp["warning"] = "口令较短：派生规则无盐，弱口令易被离线爆破，建议改用长随机串（不强制）"
	}
	c.JSON(http.StatusOK, resp)
}

type loginRequest struct {
	Password string `json:"password"`
}

// Login POST /api/v1/auth/login（免鉴权）：口令换管理密钥。
func Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	if !middleware.IsPBRInitialized() {
		apierr.Write(c, http.StatusConflict, apierr.CodeNotInitialized, "gateway is not initialized", "POST /api/v1/setup first")
		return
	}
	if delay := loginBackoff.wait(); delay > 0 {
		// 本地退避（token-spec §2.3）：连续失败后指数延迟，上限 30s；只延迟不锁号。
		time.Sleep(delay)
	}
	adminKey := model.DeriveAdminKey(req.Password)
	if !middleware.VerifyPBRAdminKey(adminKey) {
		loginBackoff.fail()
		apierr.Write(c, http.StatusUnauthorized, apierr.CodeUnauthorized, "invalid password", "")
		return
	}
	loginBackoff.succeed()
	// 签发 HttpOnly 会话 Cookie（浏览器用）；同时返回管理密钥供 AI/脚本直接取用。
	middleware.IssueAdminSession(c)
	c.JSON(http.StatusOK, gin.H{"token": adminKey, "admin_key": adminKey})
}

// Logout POST /api/v1/auth/logout（需鉴权或幂等）：清除会话 Cookie。
func Logout(c *gin.Context) {
	middleware.ClearAdminSession(c)
	c.JSON(http.StatusOK, gin.H{"logged_out": true})
}

// SessionStatus GET /api/v1/auth/session（免鉴权）：当前请求是否持有有效会话。
//
// 控制台用它做启动时的"是否已登录"判定：
//   - 200 + {authenticated:true}：Cookie 有效，前端可直接进入
//   - 200 + {authenticated:false, stale:false}：从未登录 / 无 Cookie，前端跳登录页
//   - 200 + {authenticated:false, stale:true}：**带了 Cookie 但已失效**（口令变更等），
//     此时服务端**自己下发清除 Cookie**，前端只需清本地态并跳登录页
//
// 刻意不用 401：这是"查询状态"而非"受保护资源"，用 200 承载布尔值，
// 免得前端把正常未登录误报成错误 toast。stale 的契约见 token-spec §2.5.1。
//
// 失效 Cookie 由**服务端**负责清除（对齐上游 new-api 的 RefreshAuth 口径：失败即
// ClearRefreshCookie），不把清理责任推给用户手动清浏览器数据。前端若也清一次只是兜底。
func SessionStatus(c *gin.Context) {
	state := middleware.AdminSessionCookieState(c)
	if state.Stale() {
		// 失效 Cookie 立即作废：否则它会随每个请求继续发送（Path=/），
		// 让"登录成功"之后的所有业务请求仍然 401。
		middleware.ClearAdminSession(c)
	}
	c.JSON(http.StatusOK, gin.H{
		"authenticated": state.Valid,
		"stale":         state.Stale(),
	})
}

type passwordChangeRequest struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

// ChangePassword POST /api/v1/auth/password（需鉴权）：变更口令，旧管理密钥立即失效。
//
// 例外：`PBR_ADMIN_KEY`/`PBR_ADMIN_KEYS` 生效时**拒绝**改口令。环境变量优先于库内派生值
// （token-spec §2.4），改口令既不改变实际生效的密钥、也不废掉旧会话；若返回 updated:true，
// 运维会误以为旧密钥已失效（token-spec §2.3 承诺"旧密钥立即失效"）。
func ChangePassword(c *gin.Context) {
	var req passwordChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	if len(middleware.PBRAdminKeysFromEnv()) > 0 {
		apierr.Conflict(c, apierr.CodeConflict,
			"password change is disabled while PBR_ADMIN_KEY/PBR_ADMIN_KEYS is set",
			"unset the environment variable (or rotate the key there) to manage the credential by password")
		return
	}
	if !middleware.VerifyPBRAdminKey(model.DeriveAdminKey(req.Current)) {
		apierr.Write(c, http.StatusUnauthorized, apierr.CodeUnauthorized, "invalid current password", "")
		return
	}
	if req.New == "" {
		apierr.Validation(c, "password is required")
		return
	}
	if _, err := model.SetPBRAdminPassword(req.New); err != nil {
		writeAPIError(c, err)
		return
	}
	// 口令变了，签名材料随之变化，旧会话全部失效；给当前浏览器续签一个新会话。
	middleware.IssueAdminSession(c)
	writeAudit(c, "update", "admin_credential", "admin")
	c.JSON(http.StatusOK, gin.H{"updated": true})
}

const (
	// 不设硬性最小长度（业主明确要求"不限制位数"）：空口令直接拒绝，其余一律接受。
	// 低于建议长度仍接受，但响应里给出明确提示（W8 安全项要求"弱口令有明确提示"）。
	recommendedPasswordLen = 16
	maxLoginDelay          = 30 * time.Second
)

// loginBackoff 登录失败退避：连续失败 2^(n-1) 秒，上限 30s。进程内状态，重启清空。
type loginBackoffState struct {
	mu           sync.Mutex
	failures     int
	blockedUntil time.Time
}

var loginBackoff loginBackoffState

func (l *loginBackoffState) wait() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.blockedUntil.IsZero() {
		return 0
	}
	remaining := time.Until(l.blockedUntil)
	if remaining <= 0 {
		return 0
	}
	if remaining > maxLoginDelay {
		return maxLoginDelay
	}
	return remaining
}

func (l *loginBackoffState) fail() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures++
	delay := time.Second << uint(min(l.failures-1, 5))
	if delay > maxLoginDelay {
		delay = maxLoginDelay
	}
	l.blockedUntil = time.Now().Add(delay)
}

func (l *loginBackoffState) succeed() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures = 0
	l.blockedUntil = time.Time{}
}
