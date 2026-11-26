package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/webhook"
)

func setOptionForTest(t *testing.T, key, value string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	previous, existed := common.OptionMap[key]
	common.OptionMap[key] = value
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if existed {
			common.OptionMap[key] = previous
		} else {
			delete(common.OptionMap, key)
		}
		common.OptionMapRWMutex.Unlock()
	})
}

// 基座 GET /api/option/ 是保留端点，但 webhook secret 只能经 /api/webhooks
// 明文管理；这里必须回显掩码，不能出现第二份明文出口。
func TestGetOptionsMasksWebhookSecret(t *testing.T) {
	const secret = "super-secret-value"
	setOptionForTest(t, webhook.OptionKey,
		`[{"name":"ops","url":"https://example.test/hook","secret":"`+secret+`","enabled":true,"events":[]}]`)

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/option/", nil)

	GetOptions(context)

	require.Equal(t, http.StatusOK, response.Code)
	body := response.Body.String()
	assert.NotContains(t, body, secret)
	assert.Contains(t, body, webhook.OptionKey)
	assert.Contains(t, body, "****alue")
}

// secret 的写入口只有 /api/webhooks（带 URL/事件白名单校验）。基座 PUT
// 必须拒绝该键，避免绕过校验，也避免前端把掩码值回写覆盖真实 secret。
func TestUpdateOptionRejectsWebhookTargets(t *testing.T) {
	setOptionForTest(t, webhook.OptionKey, `[{"name":"keep","url":"https://example.test/hook","secret":"real-secret","enabled":true}]`)

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/",
		strings.NewReader(`{"key":"PBRWebhookTargets","value":"[]"}`),
	)

	UpdateOption(context)

	require.Equal(t, http.StatusBadRequest, response.Code)
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.Equal(t, "validation_failed", payload.Error.Code)

	common.OptionMapRWMutex.RLock()
	stored := common.OptionMap[webhook.OptionKey]
	common.OptionMapRWMutex.RUnlock()
	assert.Contains(t, stored, "real-secret")
}
