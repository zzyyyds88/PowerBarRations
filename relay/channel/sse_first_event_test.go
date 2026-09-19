package channel

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routing-spec §3.5 / §8：流式首事件超时只在解析出**首个有效 SSE 事件**时解除。
// 注释行、心跳、空 data 都不解除——上游只发保活帧装作活着时必须照常超时换人。

func TestSSEFirstEventDetectorIgnoresKeepalives(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"注释行", ": ping\n\n"},
		{"event ping 名下的 data", "event: ping\ndata: {}\n\n"},
		{"type=ping 的心跳 data", "data: {\"type\":\"ping\"}\n\n"},
		{"ping 字段的心跳 data", "data: {\"ping\":true}\n\n"},
		{"空 data", "data:\n\n"},
		{"只有 id/retry 字段行", "id: 1\nretry: 1000\n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var d sseFirstEventDetector
			assert.False(t, d.feed([]byte(tc.input)), "保活帧不得解除首事件计时")
		})
	}
}

func TestSSEFirstEventDetectorReleasesOnRealEvent(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"真实 data 事件", "data: {\"choices\":[]}\n\n"},
		{"[DONE] 终止事件", "data: [DONE]\n\n"},
		{"非 SSE 裸 JSON", "{\"error\":\"boom\"}\n"},
		{"非 SSE 裸数组", "[{\"a\":1}]\n"},
		{"心跳后跟真实事件", "data: {\"type\":\"ping\"}\n\ndata: {\"ok\":true}\n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var d sseFirstEventDetector
			assert.True(t, d.feed([]byte(tc.input)), "真实事件必须解除首事件计时")
		})
	}
}

// 跨 Read 边界的半行必须缓存：首个事件被切成多段时仍能识别。
func TestSSEFirstEventDetectorHandlesSplitChunks(t *testing.T) {
	var d sseFirstEventDetector
	require.False(t, d.feed([]byte("da")))
	require.False(t, d.feed([]byte("ta: {\"cho")))
	assert.True(t, d.feed([]byte("ices\":[]}\n")))
}

// 单行超过上限时按已读前缀判定并丢弃剩余，拒绝无界缓冲。
func TestSSEFirstEventDetectorBoundsLineBuffer(t *testing.T) {
	var d sseFirstEventDetector
	// 超长注释行按前缀判定为非事件，缓冲被丢弃而不是无界增长。
	huge := ": " + strings.Repeat("x", sseMaxTrackedLine+100) + "\n"
	assert.False(t, d.feed([]byte(huge)))
	assert.LessOrEqual(t, len(d.partial), sseMaxTrackedLine, "半行缓冲必须有界")
}

// pbrTimeoutBody 集成：流式只认有效事件解除；非流式沿用原"首字节即解除"。
func TestPBRTimeoutBodyReleasesOnFirstValidEvent(t *testing.T) {
	newBody := func(chunks []string, stream bool) (*pbrTimeoutBody, *pbrAttemptTimeout) {
		var readers []io.Reader
		for _, chunk := range chunks {
			readers = append(readers, strings.NewReader(chunk))
		}
		timeout := &pbrAttemptTimeout{stopOnFirstRead: stream}
		return &pbrTimeoutBody{ReadCloser: io.NopCloser(io.MultiReader(readers...)), timeout: timeout}, timeout
	}

	// 流式：心跳帧不解除。
	body, timeout := newBody([]string{"data: {\"type\":\"ping\"}\n\n"}, true)
	_, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.False(t, timeout.stopped, "只有心跳不得解除首事件计时")

	// 流式：真实事件解除。
	body, timeout = newBody([]string{"data: {\"ok\":true}\n\n"}, true)
	_, err = io.ReadAll(body)
	require.NoError(t, err)
	assert.True(t, timeout.stopped, "真实事件必须解除")

	// 非流式：首个字节即解除（stopOnFirstRead=false 时 release 是无操作，但语义仍成立）。
	body, timeout = newBody([]string{"data: {\"type\":\"ping\"}\n\n"}, false)
	_, err = io.ReadAll(body)
	require.NoError(t, err)
	assert.False(t, timeout.stopped, "非流式不设置 stopped（由整响应超时控制）")
}
