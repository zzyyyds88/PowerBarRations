package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
