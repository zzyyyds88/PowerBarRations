package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/internal/session"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 审查整改回归（token-spec §2.3）：管理面必须同时接受
//   - `Authorization: Bearer <管理密钥>`（AI/脚本），
//   - HttpOnly 会话 Cookie（浏览器），
//
// 且两者都缺失/错误时返回 401。
//
// 这里用 PBR_ADMIN_KEY 环境变量路径，避免依赖数据库初始化。
func TestPBRAuthAcceptsBearerAndSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(PBREnvAdminKey, "env-admin-key-value")
	t.Setenv(PBREnvAdminKeys, "")

	// 会话签名材料 = HashAdminKey(env key)。
	material := model.HashAdminKey("env-admin-key-value")
	token, err := session.Issue(material, time.Hour, time.Now())
	require.NoError(t, err)

	newRouter := func() *gin.Engine {
		engine := gin.New()
		engine.GET("/x", PBRAuth(), func(c *gin.Context) { c.String(http.StatusOK, "ok") })
		return engine
	}

	do := func(build func(*http.Request)) int {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		build(req)
		recorder := httptest.NewRecorder()
		newRouter().ServeHTTP(recorder, req)
		return recorder.Code
	}

	// 1) Bearer 管理密钥 → 200（AI 通道）
	assert.Equal(t, http.StatusOK, do(func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer env-admin-key-value")
	}), "Bearer 管理密钥必须可用")

	// 2) 会话 Cookie → 200（浏览器通道）
	assert.Equal(t, http.StatusOK, do(func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	}), "会话 Cookie 必须可用")

	// 3) 两者都缺 → 401
	assert.Equal(t, http.StatusUnauthorized, do(func(_ *http.Request) {}), "无凭据必须 401")

	// 4) 错误 Bearer → 401
	assert.Equal(t, http.StatusUnauthorized, do(func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer wrong")
	}), "错误管理密钥必须 401")

	// 5) 伪造/篡改的会话 Cookie → 401（不能绕过签名）
	assert.Equal(t, http.StatusUnauthorized, do(func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: token + "x"})
	}), "篡改的会话令牌必须 401")
}

// 会话令牌在口令变更后必须失效：签名材料来自管理凭据，换材料即验不过。
// 用两把不同的环境变量管理密钥模拟"改口令"。
func TestPBRAuthSessionInvalidatedByCredentialChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(PBREnvAdminKeys, "")

	// 用第一把密钥签发的会话。
	t.Setenv(PBREnvAdminKey, "first-key")
	firstMaterial := model.HashAdminKey("first-key")
	token, err := session.Issue(firstMaterial, time.Hour, time.Now())
	require.NoError(t, err)

	engine := gin.New()
	engine.GET("/x", PBRAuth(), func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	// 换成第二把密钥（等价于改口令）后，旧会话必须 401。
	t.Setenv(PBREnvAdminKey, "second-key")
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code, "改凭据后旧会话必须失效")
}
