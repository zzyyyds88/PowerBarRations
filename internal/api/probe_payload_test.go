package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/testutil/fakeupstream"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 探活 payload 的 max_tokens 必须给推理模型留出推理 + 输出空间：
// max_tokens=1 会让 DeepSeek/o 系等推理渠道在 1 个 token 内无法完成推理，
// 上游直接回 502，把健康渠道探成 soft_transient 误报
// （2026-11-21 生产 tokenav 渠道实证：max_tokens=1 → 502，128 → 200）。
func TestProbePayloadLeavesRoomForReasoning(t *testing.T) {
	db := setupAPITestDB(t)
	fake := fakeupstream.New(fakeupstream.Config{})
	t.Cleanup(fake.Close)

	baseURL := fake.URL
	ch := &model.Channel{Name: "probe-ch", Type: 1, Key: "sk-probe",
		Status: common.ChannelStatusEnabled, Group: "default",
		Models: "probe-model", BaseURL: &baseURL}
	require.NoError(t, db.Create(ch).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "name", Value: "probe-ch"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/channels/probe-ch/test", nil)
	TestChannel(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	last, ok := fake.LastRequest()
	require.True(t, ok, "假上游应收到探活请求")
	assert.Equal(t, "/v1/chat/completions", last.Path)

	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(last.Body), &body))
	assert.Equal(t, "probe-model", body["model"])
	assert.EqualValues(t, 128, body["max_tokens"], "max_tokens 必须容纳推理模型的推理 + 输出")
	assert.Equal(t, false, body["stream"])

	var resp map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.Equal(t, "success", resp["status"], "假上游正常回包时探活应为 success")
}
