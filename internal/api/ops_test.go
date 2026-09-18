package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 运维端点用渠道名寻址：适配层必须把 {name} 解析成 id 并注入基座 handler 需要的字段。
func TestOpsAdapterResolvesChannelNameToID(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "ops-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "m"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, ch.AddAbilities(nil))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "name", Value: "ops-ch"}}
	// 基座 ManageMultiKeys 需要 body 里的 channel_id；适配层应注入。
	body := `{"action":"get_key_status"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/channels/ops-ch/multi-keys", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	ManageMultiKeysByName(c)

	// 单密钥渠道会返回业务失败，但**不能**是"渠道不存在"——说明名字已解析成功。
	require.NotEqual(t, http.StatusNotFound, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "channel_not_found")
}

// 渠道不存在时适配层返回 404 channel_not_found（不进入基座 handler）。
func TestOpsAdapterRejectsUnknownChannel(t *testing.T) {
	setupAPITestDB(t)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "name", Value: "ghost"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/ghost/key", nil)

	GetChannelKeyByName(c)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "channel_not_found")
}

// by-tag/status 按 status 分派：启用走 EnableTagChannels，否则走 Disable。
func TestBatchChannelTagStatusDispatches(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "tag-ch", Type: 1, Key: "sk", Status: common.ChannelStatusManuallyDisabled, Group: "default", Models: "m"}
	tag := "t1"
	ch.Tag = &tag
	require.NoError(t, db.Create(ch).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/channels/by-tag/status",
		strings.NewReader(`{"tag":"t1","status":1}`))
	c.Request.Header.Set("Content-Type", "application/json")

	BatchChannelTagStatus(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	t.Logf("by-tag/status response: %s", recorder.Body.String())
	assert.Equal(t, true, resp["success"], recorder.Body.String())
	var saved model.Channel
	require.NoError(t, db.Where("name = ?", "tag-ch").First(&saved).Error)
	assert.Equal(t, common.ChannelStatusEnabled, saved.Status, "status=1 应启用该标签下的渠道")
}

// 系统任务详情：契约路径 {id} → 基座 task_id。
func TestGetSystemTaskByIDMapsParam(t *testing.T) {
	setupAPITestDB(t)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "42"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/system-tasks/42", nil)

	GetSystemTaskByID(c)
	// 任务不存在时应为业务失败而不是参数缺失导致的 400。
	assert.NotEqual(t, http.StatusBadRequest, recorder.Code)
}
