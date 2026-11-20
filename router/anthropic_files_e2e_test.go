package router

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/testutil/fakeupstream"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/setting"
)

// Anthropic Files 端到端（ADR 0011）：上传不要求车道头 → /v1/messages 引用
// file_id → 上游收到的是内联 base64 image_url，而不是 file_id。
//
// 这条链路是本次修复的核心价值：DSH 每天 400+ 次上传此前全部被 400 拒绝，
// 只能降级成内联 base64；现在上传真的成功，且引用能被正确展开。

func newAnthropicFilesE2EEngine(t *testing.T, upstreamURL string, laneName string) *gin.Engine {
	t.Helper()
	// 同进程内其它测试可能改过限流全局开关；关掉它避免 NewTicker(0) panic。
	setting.ModelRequestRateLimitEnabled = false
	// 测试进程未加载运行配置：流式扫描器的 ticker 依赖这个值，0 会 panic。
	if constant.StreamingTimeout == 0 {
		constant.StreamingTimeout = 30
	}
	baseURL := upstreamURL
	channel := &model.Channel{
		Name:    "files-e2e-ch",
		Type:    constant.ChannelTypeOpenAI,
		Key:     "sk-test",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
		Group:   "default",
		Models:  laneName,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	lane := &model.Lane{Name: laneName, Enabled: true, Mode: model.LaneModeFailover}
	require.NoError(t, model.DB.Create(lane).Error)
	require.NoError(t, model.DB.Create(&model.LaneMember{
		LaneId: lane.Id, ChannelId: channel.Id, Model: laneName, Priority: 1,
	}).Error)

	engine := gin.New()
	SetPBRRouter(engine)
	SetRelayRouter(engine)
	return engine
}

func uploadAnthropicFile(t *testing.T, engine http.Handler, content []byte) string {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="shot.png"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("expires_after[anchor]", "created_at"))
	require.NoError(t, writer.WriteField("expires_after[seconds]", "3600"))
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/v1/files", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	// 只带 beta 头，不带 X-PBR-Lane——这是 DSH 的真实行为。
	request.Header.Set("anthropic-beta", "files-api-2025-04-14")
	request.Header.Set("Authorization", "Bearer anthropic-files-e2e-key")

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var obj map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &obj))
	id, _ := obj["id"].(string)
	require.NotEmpty(t, id)
	return id
}

func TestAnthropicFilesEndToEndUploadAndExpand(t *testing.T) {
	t.Setenv("PBR_ADMIN_KEY", "anthropic-files-e2e-admin-key")
	t.Setenv("PBR_FILES_DIR", t.TempDir())
	setupRelayRouterTestDB(t)
	migratePBRMetricsTestTables(t)
	require.NoError(t, model.AutoMigratePBRFiles(model.DB))
	createTestClientKey(t, "anthropic-files-e2e-key", "anthropic-files-e2e-key")

	const laneName = "files-e2e-lane"
	upstream := fakeupstream.New(fakeupstream.Config{
		Behaviors: map[string]fakeupstream.Behavior{
			laneName: {Body: `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"` + laneName + `","choices":[{"index":0,"message":{"role":"assistant","content":"看到了"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`},
		},
	})
	t.Cleanup(upstream.Close)

	engine := newAnthropicFilesE2EEngine(t, upstream.URL, laneName)

	// 第一步：上传成功（此前这里是 400 missing X-PBR-Lane header）。
	imageBytes := []byte("\x89PNG\r\n\x1a\nfake-screenshot-bytes")
	fileID := uploadAnthropicFile(t, engine, imageBytes)

	// 第二步：messages 引用该 file_id。
	messagesBody, err := json.Marshal(map[string]any{
		"model":      laneName,
		"max_tokens": 64,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "看这张截图"},
					map[string]any{
						"type":   "image",
						"source": map[string]any{"type": "file", "file_id": fileID},
					},
				},
			},
		},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(messagesBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer anthropic-files-e2e-key")
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	// 第三步：断言上游实际收到的是 base64，而不是 file_id。
	requests := upstream.Requests()
	var chatBody string
	for _, recorded := range requests {
		if strings.HasSuffix(recorded.Path, "/chat/completions") {
			chatBody = recorded.Body
			break
		}
	}
	require.NotEmpty(t, chatBody, "上游必须收到一次 chat 请求")

	encoded := chatBody
	assert.Contains(t, encoded, "data:image/png;base64,", "图片必须被展开成内联 base64")
	assert.NotContains(t, encoded, fileID, "网关内部对象 ID 不得泄漏给上游")
}

// 过期 file_id 必须得到明确 400（客户端据此重传），而不是静默缺图。
func TestAnthropicFilesEndToEndExpiredReferenceFails(t *testing.T) {
	t.Setenv("PBR_ADMIN_KEY", "anthropic-files-e2e-admin-key-2")
	t.Setenv("PBR_FILES_DIR", t.TempDir())
	setupRelayRouterTestDB(t)
	migratePBRMetricsTestTables(t)
	require.NoError(t, model.AutoMigratePBRFiles(model.DB))
	createTestClientKey(t, "anthropic-files-e2e-key", "anthropic-files-e2e-key")

	const laneName = "files-e2e-lane-2"
	upstream := fakeupstream.New(fakeupstream.Config{})
	t.Cleanup(upstream.Close)

	engine := newAnthropicFilesE2EEngine(t, upstream.URL, laneName)
	fileID := uploadAnthropicFile(t, engine, []byte("bytes"))

	// 把对象改成已过期。
	require.NoError(t, model.DB.Model(&model.PBRFile{}).
		Where("object_id = ?", fileID).
		Update("expires_at", 1).Error)

	messagesBody, err := json.Marshal(map[string]any{
		"model":      laneName,
		"max_tokens": 64,
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{map[string]any{
				"type":   "image",
				"source": map[string]any{"type": "file", "file_id": fileID},
			}},
		}},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(messagesBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer anthropic-files-e2e-key")
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), fileID)
	assert.Contains(t, recorder.Body.String(), "has expired")
	// 客户端可修正的问题不得打到上游。
	assert.Empty(t, upstream.Requests(), "file 引用失效时不应向上游发起请求")
}

// 空流端到端：上游 200 + 立即 EOF 必须判失败，而不是落一条成功的空响应。
func TestEmptyStreamIsNotRecordedAsSuccess(t *testing.T) {
	t.Setenv("PBR_ADMIN_KEY", "empty-stream-admin-key")
	setupRelayRouterTestDB(t)
	migratePBRMetricsTestTables(t)
	createTestClientKey(t, "empty-stream-key", "empty-stream-key")

	const laneName = "empty-stream-lane"
	upstream := fakeupstream.New(fakeupstream.Config{
		Behaviors: map[string]fakeupstream.Behavior{
			laneName: {Mode: "empty"},
		},
	})
	t.Cleanup(upstream.Close)

	engine := newAnthropicFilesE2EEngine(t, upstream.URL, laneName)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"`+laneName+`","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer empty-stream-key")
	engine.ServeHTTP(recorder, request)

	// 空流是成员级失败：不在客户端已经拿到 200 之后才"发现"，而应当返回错误状态。
	assert.NotEqual(t, http.StatusOK, recorder.Code,
		"零帧空流不得以 200 交付，body=%s", recorder.Body.String())

	var entry model.PBRRequestLog
	require.NoError(t, model.DB.Order("id DESC").First(&entry).Error)
	assert.False(t, entry.Success, "空流不得记成功")
	assert.Zero(t, entry.CompletionTokens)
}

// io.Reader 需要被引用（部分构建组合下 strings.NewReader 已覆盖，保留以防裁剪）。
var _ io.Reader = strings.NewReader("")
