package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	relayconstant "github.com/zzyyyds88/PowerBarRations/relay/constant"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	relaytypes "github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

// moderations 是独立 relay mode（design-v1 §4）：响应体必须原样透传
// （flagged/categories/category_scores 不能像 chat completion 那样被重写丢失），
// 只在存在 model 字段时按 §4.1 回填请求名。
func TestModerationsHelperPassesThroughModerationObject(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const upstreamBody = `{"id":"modr-1","model":"upstream-moderation","results":[{"flagged":true,"categories":{"hate":true},"category_scores":{"hate":0.93}}]}`
	var (
		capturedPath string
		capturedBody []byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamBody))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/moderations", nil)

	common.SetContextKey(c, constant.ContextKeyOriginalModel, "mod-lane")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, 1)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "sk-test")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeModerations,
		RelayFormat:     relaytypes.RelayFormatOpenAI,
		OriginModelName: "mod-lane",
		RequestURLPath:  "/v1/moderations",
		Request:         &dto.GeneralOpenAIRequest{Model: "mod-lane", Input: "some text"},
	}

	apiErr := ModerationsHelper(c, info)
	require.Nil(t, apiErr)
	require.Equal(t, http.StatusOK, recorder.Code)

	assert.Equal(t, "/v1/moderations", capturedPath, "moderations 必须打到独立端点")
	var upstreamRequest map[string]any
	require.NoError(t, json.Unmarshal(capturedBody, &upstreamRequest))
	assert.Equal(t, "some text", upstreamRequest["input"])

	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	// §4.1：响应 model 回填请求名（车道名），不暴露上游真名。
	assert.Equal(t, "mod-lane", response["model"])
	results, ok := response["results"].([]any)
	require.True(t, ok, "moderation 结果不得被 chat 形状重写：%s", recorder.Body.String())
	require.Len(t, results, 1)
	result, ok := results[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, result["flagged"])
	assert.Contains(t, result, "categories")
	assert.Contains(t, result, "category_scores")
}

// 上游非 200 必须按真实状态码返回（不透传 200），走统一的错误映射。
func TestModerationsHelperMapsUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"input rejected","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/moderations", nil)

	common.SetContextKey(c, constant.ContextKeyOriginalModel, "mod-lane")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, 1)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "sk-test")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeModerations,
		RelayFormat:     relaytypes.RelayFormatOpenAI,
		OriginModelName: "mod-lane",
		RequestURLPath:  "/v1/moderations",
		Request:         &dto.GeneralOpenAIRequest{Model: "mod-lane", Input: "some text"},
	}

	apiErr := ModerationsHelper(c, info)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Contains(t, apiErr.Error(), "input rejected")
}
