package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/middleware"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 环境变量管理密钥生效时不得改口令（审查 F14）：
// PBR_ADMIN_KEY/PBR_ADMIN_KEYS 优先于库内派生值，改口令既不改变生效密钥也不废旧会话，
// 返回 updated:true 会让人误以为旧密钥已失效（token-spec §2.3）。
func TestChangePasswordRefusedWhenEnvAdminKeyIsSet(t *testing.T) {
	setupAPITestDB(t)
	t.Setenv(middleware.PBREnvAdminKey, "env-admin-key")
	require.NotEmpty(t, middleware.PBRAdminKeysFromEnv())

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/password",
		strings.NewReader(`{"current":"whatever","new":"new-password"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	ChangePassword(c)

	assert.Equal(t, http.StatusConflict, recorder.Code)
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, apierr.CodeConflict, body.Error.Code)
}

// 没有环境变量覆盖时，改口令仍必须可用（守卫不能把正常路径一起挡掉）。
func TestChangePasswordStillWorksWithoutEnvOverride(t *testing.T) {
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PBRAdminCredential{}))
	t.Setenv(middleware.PBREnvAdminKey, "")
	t.Setenv(middleware.PBREnvAdminKeys, "")

	adminKey, err := model.SetPBRAdminPassword("old-password")
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	// 旧口令错 → 401。
	badRecorder := httptest.NewRecorder()
	badCtx, _ := gin.CreateTestContext(badRecorder)
	badCtx.Request = httptest.NewRequest(http.MethodPost, "/api/auth/password",
		strings.NewReader(`{"current":"wrong","new":"new-password"}`))
	badCtx.Request.Header.Set("Content-Type", "application/json")
	ChangePassword(badCtx)
	assert.Equal(t, http.StatusUnauthorized, badRecorder.Code)

	// 旧口令对 → 200，且新管理密钥生效、旧密钥失效。
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/password",
		strings.NewReader(`{"current":"old-password","new":"new-password"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	ChangePassword(c)
	assert.Equal(t, http.StatusOK, recorder.Code)

	assert.False(t, model.VerifyPBRAdminKey(adminKey), "旧管理密钥必须立即失效")
	assert.True(t, model.VerifyPBRAdminKey(model.DeriveAdminKey("new-password")))
}

// 非法游标返回 400 validation_failed，而不是静默回退第一页（审查 F16 / api-spec §2.4）。
func TestListEndpointsRejectInvalidCursor(t *testing.T) {
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ClientKey{}, &model.PBRRequestLog{}, &model.PBRAuditLog{}))
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name    string
		handler gin.HandlerFunc
	}{
		{"channels", ListChannels},
		{"lanes", ListLanes},
		{"keys", ListKeys},
		{"logs", ListLogs},
		{"audit", ListAudit},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/?cursor=!!!not-base64!!!", nil)
		tc.handler(c)
		assert.Equalf(t, http.StatusBadRequest, recorder.Code, "%s: 非法游标必须 400", tc.name)
		assert.Containsf(t, recorder.Body.String(), apierr.CodeValidationFailed, "%s", tc.name)
	}

	// 合法游标（空串）仍然正常返回。
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	ListChannels(c)
	assert.Equal(t, http.StatusOK, recorder.Code)
}
