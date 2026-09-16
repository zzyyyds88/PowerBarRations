package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pbr/common"
	"pbr/internal/session"
	"pbr/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 审查整改回归（token-spec §2.3）：登录/首启必须签发 HttpOnly 会话 Cookie，
// 使浏览器无需保存管理密钥；同时仍返回派生管理密钥供 AI/脚本使用。
func TestLoginIssuesHttpOnlySessionCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PBRAdminCredential{}))
	_, err := model.SetPBRAdminPassword("Login-Issue-Pass-2026")
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"password":"Login-Issue-Pass-2026"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Login(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1, "登录必须下发一个会话 Cookie")
	cookie := cookies[0]
	assert.Equal(t, session.CookieName, cookie.Name)
	assert.True(t, cookie.HttpOnly, "会话 Cookie 必须 HttpOnly（JS 不可读）")
	assert.NotEmpty(t, cookie.Value)
	// 响应体仍带管理密钥（供 AI），但 Cookie 里不含它。
	assert.Contains(t, recorder.Body.String(), "admin_key")
	assert.NotContains(t, cookie.Value, "Login-Issue-Pass-2026")
}

// 登出必须清除会话 Cookie。
func TestLogoutClearsSessionCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAPITestDB(t)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	Logout(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, -1, cookies[0].MaxAge)
	assert.Empty(t, cookies[0].Value)
}

// 改口令必须给当前浏览器续签新会话（否则改完口令自己就被踢出）。
func TestChangePasswordReissuesSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PBRAdminCredential{}))
	_, err := model.SetPBRAdminPassword("Old-Pass-2026")
	require.NoError(t, err)
	_ = common.SessionCookieSecure

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/password",
		strings.NewReader(`{"current":"Old-Pass-2026","new":"New-Pass-2026"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	ChangePassword(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1, "改口令后必须续签当前会话")
	assert.Equal(t, session.CookieName, cookies[0].Name)
	assert.NotEmpty(t, cookies[0].Value)
}

// token-spec §2.5.1：会话状态必须区分"没登录"与"带了 Cookie 但已失效"。
//
// 真实故障现场：管理口令变更后旧 Cookie 立即失效，但前端只看 authenticated=false，
// 于是"登录成功却每个请求都 401"。这里锁定 stale 字段的契约。
func TestSessionStatusDistinguishesStaleCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PBRAdminCredential{}))

	// 用第一版口令签发的会话 Cookie。
	firstKey, err := model.SetPBRAdminPassword("First-Pass-2026")
	require.NoError(t, err)
	staleCookie, err := session.Issue(model.HashAdminKey(firstKey), time.Hour, time.Now())
	require.NoError(t, err)

	// 改口令 → 签名材料变化，旧 Cookie 失效。
	_, err = model.SetPBRAdminPassword("Second-Pass-2026")
	require.NoError(t, err)

	status := func(cookie string) map[string]any {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
		if cookie != "" {
			c.Request.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
		}
		SessionStatus(c)
		require.Equal(t, http.StatusOK, recorder.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
		return body
	}

	// 1) 没带 Cookie：未登录，且不是 stale。
	none := status("")
	assert.Equal(t, false, none["authenticated"])
	assert.Equal(t, false, none["stale"], "没带 Cookie 不得报 stale")

	// 2) 带了失效 Cookie：未登录 + stale=true（前端据此清态并提示"凭据已变更"）。
	stale := status(staleCookie)
	assert.Equal(t, false, stale["authenticated"])
	assert.Equal(t, true, stale["stale"], "口令变更后的旧 Cookie 必须标记为 stale")

	// 3) 用新口令签发的新 Cookie：有效，且不是 stale。
	secondKey := model.DeriveAdminKey("Second-Pass-2026")
	fresh, err := session.Issue(model.HashAdminKey(secondKey), time.Hour, time.Now())
	require.NoError(t, err)
	valid := status(fresh)
	assert.Equal(t, true, valid["authenticated"])
	assert.Equal(t, false, valid["stale"], "有效会话不得报 stale")
}

// token-spec §2.5.1：服务端必须在判定 stale 时**自己清除**失效 Cookie，
// 而不是把清理责任推给用户（对齐上游 new-api 的 RefreshAuth 失败即清 Cookie）。
func TestSessionStatusClearsStaleCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PBRAdminCredential{}))

	firstKey, err := model.SetPBRAdminPassword("Clear-First-2026")
	require.NoError(t, err)
	staleCookie, err := session.Issue(model.HashAdminKey(firstKey), time.Hour, time.Now())
	require.NoError(t, err)

	// 改口令 → 旧 Cookie 失效。
	_, err = model.SetPBRAdminPassword("Clear-Second-2026")
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	c.Request.AddCookie(&http.Cookie{Name: session.CookieName, Value: staleCookie})
	SessionStatus(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"stale":true`)

	// 关键断言：响应必须下发清除该 Cookie 的 Set-Cookie。
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1, "判定 stale 时必须下发清除 Cookie")
	assert.Equal(t, session.CookieName, cookies[0].Name)
	assert.Empty(t, cookies[0].Value, "清除 Cookie 的值必须为空")
	assert.Equal(t, -1, cookies[0].MaxAge, "清除 Cookie 的 MaxAge 必须为 -1")
}

// 有效会话不得被误清（否则用户每次刷新都掉线）。
func TestSessionStatusKeepsValidCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PBRAdminCredential{}))
	key, err := model.SetPBRAdminPassword("Keep-Valid-2026")
	require.NoError(t, err)
	valid, err := session.Issue(model.HashAdminKey(key), time.Hour, time.Now())
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	c.Request.AddCookie(&http.Cookie{Name: session.CookieName, Value: valid})
	SessionStatus(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"authenticated":true`)
	assert.Empty(t, recorder.Result().Cookies(), "有效会话不得下发清除 Cookie")
}
