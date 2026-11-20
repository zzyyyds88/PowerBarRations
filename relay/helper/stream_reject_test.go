package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	relaycommon "github.com/zzyyyds88/PowerBarRations/relay/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

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
		// Anthropic：`event: error` 的 data 帧是 type=error + error 对象；
		// 扫描器只投递 data，因此按 type/error 判定即覆盖该事件。
		{"Anthropic event:error 的 data 帧", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, true, "Overloaded"},
		{"Anthropic 只有 error 对象（无 type）", `{"error":{"type":"api_error","message":"upstream exploded"}}`, true, "upstream exploded"},
		// Gemini：流式 JSON 帧只需非空 error 对象。
		{"Gemini 错误帧", `{"error":{"code":400,"message":"User location is not supported","status":"INVALID_ARGUMENT"}}`, true, "User location is not supported"},
		{"Gemini error:null 不算", `{"candidates":[],"error":null}`, false, ""},
		// Responses：response.failed + 非空 response.error 才是错误帧。
		{"Responses response.failed + response.error", `{"type":"response.failed","response":{"id":"r1","error":{"code":"server_error","message":"model overloaded"}}}`, true, "model overloaded"},
		{"Responses response.failed 无 error 不算（正常终态事件）", `{"type":"response.failed","response":{"id":"r1","status":"failed"}}`, false, ""},
		{"Responses response.failed + error:null 不算", `{"type":"response.failed","response":{"id":"r1","error":null}}`, false, ""},
		{"Responses 正常事件不算", `{"type":"response.output_text.delta","delta":"hi"}`, false, ""},
		{"Responses response.created 不算", `{"type":"response.created","response":{"id":"r1","error":null}}`, false, ""},
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

	// 心跳帧（OpenAI ping / Responses 生命周期事件）不算错误。
	for _, heartbeat := range []string{`{"type":"ping"}`, `{"type":"response.created","response":{}}`, `{"ping":true}`} {
		_, isErr := IsUpstreamErrorFrame([]byte(heartbeat))
		assert.False(t, isErr, "心跳帧不得判成错误帧: %s", heartbeat)
	}
}

// 提交前拒绝统一走这条错误链：upstream_reject + 400，供车道换成员（routing-spec §4.1）。
func TestNewUpstreamStreamRejectError(t *testing.T) {
	err := NewUpstreamStreamRejectError("content blocked")
	require.NotNil(t, err)
	assert.Equal(t, types.ErrorCodeUpstreamReject, err.GetErrorCode(),
		"必须复用 upstream_reject 错误码，控制器据此换成员且不冷却")
	assert.Equal(t, http.StatusBadRequest, err.StatusCode)
	assert.Contains(t, err.Error(), "content blocked")
}

// 提交后留痕：只写上下文键，不改响应、不改错误（controller 收尾读该键写日志）。
func TestMarkStreamReject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	MarkStreamReject(c, "")
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject), "空报文不写键")

	MarkStreamReject(c, "overloaded")
	assert.Equal(t, "overloaded", common.GetContextKeyString(c, constant.ContextKeyPBRStreamReject))
	MarkStreamReject(nil, "ignored")
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

// Claude 入站（/v1/messages）的流式主出口也必须打提交标记与首字耗时。
//
// 这里是线上"首字恒为不适用"的根因：ClaudeData 写的是真正的 `data:` 帧，
// 却漏了 MarkStreamCommitted，于是 ttft_ms 永远为 0，且提交后的流内 error 帧
// 会被误判成"提交前"。
func TestClaudeDataMarksCommitAndFirstByte(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name  string
		write func(*gin.Context) error
	}{
		{"ClaudeData", func(c *gin.Context) error {
			return ClaudeData(c, dto.ClaudeResponse{Type: "message_start"})
		}},
		{"ClaudeChunkData", func(c *gin.Context) error {
			ClaudeChunkData(c, dto.ClaudeResponse{Type: "content_block_delta"}, `{"type":"content_block_delta"}`)
			return nil
		}},
		{"ResponseChunkData", func(c *gin.Context) error {
			return ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.output_text.delta"}, `{"delta":"hi"}`)
		}},
		{"ObjectData", func(c *gin.Context) error {
			return ObjectData(c, map[string]any{"choices": []any{}})
		}},
		{"Done", func(c *gin.Context) error {
			Done(c)
			return nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now().Add(-120*time.Millisecond))

			require.NoError(t, tc.write(c))

			assert.True(t, StreamCommitted(c), "写出 data: 帧即提交（提交后不得再故障转移）")
			first := common.GetContextKeyInt(c, constant.ContextKeyPBRTTFTMs)
			require.GreaterOrEqual(t, first, 100, "首字耗时必须在第一帧写出时打点")
			require.Less(t, first, 5000)
			assert.False(t, StreamFailedWithoutFrames(c, &relaycommon.RelayInfo{}),
				"已提交的流不是空流")
		})
	}
}

// 空流判定：上游建流但零有效帧，且未向客户端提交任何内容。
func TestStreamFailedWithoutFrames(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	assert.False(t, StreamFailedWithoutFrames(c, nil), "没有 RelayInfo 不判定")
	assert.True(t, StreamFailedWithoutFrames(c, &relaycommon.RelayInfo{}),
		"未提交 + 零帧 = 上游空响应")

	assert.False(t, StreamFailedWithoutFrames(c, &relaycommon.RelayInfo{ReceivedResponseCount: 1}),
		"收到过有效帧就不是空流")

	// 已提交（客户端已拿到内容）时不能改判成错误，否则会把正常流截断。
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, StringData(c2, `{"choices":[]}`))
	assert.False(t, StreamFailedWithoutFrames(c2, &relaycommon.RelayInfo{}),
		"已提交的请求不再判空流")
}

// 空流错误必须落在 bad_response 分类上，才能获得"重试同成员 + 换成员 + 冷却"。
func TestEmptyStreamErrorClassification(t *testing.T) {
	err := EmptyStreamError()
	require.NotNil(t, err)
	assert.Equal(t, types.ErrorCodeEmptyResponse, err.GetErrorCode())
	assert.Equal(t, route.KindBadResponse, route.Classify(err))
	assert.True(t, route.ShouldSwitchMember(route.Classify(err)))
}
