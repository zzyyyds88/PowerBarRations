package helper

import (
	"net/http/httptest"
	"testing"

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
