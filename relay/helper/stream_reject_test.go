package helper

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 流内错误帧判定（routing-spec §4.4）：只认明确的 error 帧形态，宁漏不误。
func TestIsUpstreamErrorFrame(t *testing.T) {
	cases := []struct {
		name    string
		data    string
		wantErr bool
		wantMsg string
	}{
		{"error 对象（内容审查典型形态）", `{"error":{"message":"Output data may contain inappropriate content.","type":"invalid_request_error","code":"data_inspection_failed"}}`, true, "Output data may contain inappropriate content."},
		{"type=error", `{"type":"error","message":"boom"}`, true, "boom"},
		{"type=upstream_error（大小写不敏感）", `{"type":"Upstream_Error"}`, true, "upstream stream returned error event"},
		{"error 为 null 不算", `{"error":null,"choices":[]}`, false, ""},
		{"正常内容帧不算", `{"choices":[{"delta":{"content":"你好"}}]}`, false, ""},
		{"只有 message 字段不算（避免误判）", `{"message":"ok"}`, false, ""},
		{"usage 帧不算", `{"usage":{"prompt_tokens":10,"completion_tokens":2}}`, false, ""},
		{"非 JSON 不算", `not-json`, false, ""},
		{"空串不算", ``, false, ""},
		{"[DONE] 不算", `[DONE]`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, isErr := IsUpstreamErrorFrame([]byte(tc.data))
			assert.Equal(t, tc.wantErr, isErr)
			if tc.wantErr {
				assert.Equal(t, tc.wantMsg, msg)
			}
		})
	}

	// error 为字符串时取其原文。
	msg, isErr := IsUpstreamErrorFrame([]byte(`{"error":"plain failure"}`))
	require.True(t, isErr)
	assert.Equal(t, "plain failure", msg)
}

// 提交标记：只有真正写出 `data:` 帧才算提交；心跳（`: PING`）不算。
func TestStreamCommittedMarking(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	assert.False(t, StreamCommitted(c), "新建上下文未提交")

	require.NoError(t, PingData(c))
	assert.False(t, StreamCommitted(c), "心跳注释帧不算提交（客户端可忽略，不能关掉故障转移）")

	require.NoError(t, StringData(c, `{"choices":[]}`))
	assert.True(t, StreamCommitted(c), "写出 data 帧即提交")

	// 新上下文不受旧标记影响（标记按请求隔离）。
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.False(t, StreamCommitted(c2))
}

// 首字耗时（TTFT）：第一帧数据写出时打点，且只打一次。
//
// 这个值决定渠道状态页"输出速度"的口径——不剔除首字等待，算出来的就只是
// 端到端速度（含排队与预填充），拿它横向比渠道会失真。
func TestMarkFirstByteRecordsOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	// 没有请求起始时间时不猜：保持为空，宁可显示"-"也不报假数。
	require.NoError(t, StringData(c, `{"choices":[]}`))
	assert.Zero(t, common.GetContextKeyInt(c, constant.ContextKeyPBRTTFTMs),
		"取不到请求起始时间就不记首字")

	// 起始时间在 120ms 前 → 首字应记为正数。
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c2, constant.ContextKeyRequestStartTime, time.Now().Add(-120*time.Millisecond))
	require.NoError(t, StringData(c2, `{"choices":[]}`))
	first := common.GetContextKeyInt(c2, constant.ContextKeyPBRTTFTMs)
	require.GreaterOrEqual(t, first, 100, "首字耗时应在 100ms 量级")
	require.Less(t, first, 5000)

	// 只记第一次：后续帧不得把首字拖后。
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, StringData(c2, `{"choices":[]}`))
	assert.Equal(t, first, common.GetContextKeyInt(c2, constant.ContextKeyPBRTTFTMs),
		"首字耗时只记第一帧")
}
