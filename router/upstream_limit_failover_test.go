package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/testutil/fakeupstream"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/setting"

	"github.com/gin-gonic/gin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 上游免费额度耗尽的故障转移端到端（routing-spec §3 第 6 步 / §4.1.1 / §4.1）。
//
// 线上实案（hermes-fund → 1412）：OpenCode 免费档的上游先发一个纯角色开场帧、
// 再在流内报 FreeUsageLimitError。旧口径下开场帧写出即提交 ⇒ 垃圾 200 空流交付
// 给客户端且记成功。内容帧口径下：开场帧暂存、错误帧构成提交前拒绝 ⇒
// hard_quota 长冷却 + 故障转移到下一个成员，客户端拿到正常响应。

func newFreeLimitFailoverEngine(t *testing.T, limitURL, healthyURL, laneName string) *gin.Engine {
	t.Helper()
	// 同进程内其它测试可能改过限流全局开关；关掉它避免 NewTicker(0) panic。
	setting.ModelRequestRateLimitEnabled = false
	// 测试进程未加载运行配置：流式扫描器的 ticker 依赖这个值，0 会 panic。
	if constant.StreamingTimeout == 0 {
		constant.StreamingTimeout = 30
	}
	baseA := limitURL
	channelA := &model.Channel{
		Name:    "limit-ch",
		Type:    constant.ChannelTypeOpenAI,
		Key:     "sk-a",
		BaseURL: &baseA,
		Status:  common.ChannelStatusEnabled,
		Group:   "default",
		Models:  laneName,
	}
	require.NoError(t, model.DB.Create(channelA).Error)
	baseB := healthyURL
	channelB := &model.Channel{
		Name:    "healthy-ch",
		Type:    constant.ChannelTypeOpenAI,
		Key:     "sk-b",
		BaseURL: &baseB,
		Status:  common.ChannelStatusEnabled,
		Group:   "default",
		Models:  laneName,
	}
	require.NoError(t, model.DB.Create(channelB).Error)
	lane := &model.Lane{Name: laneName, Enabled: true, Mode: model.LaneModeFailover}
	require.NoError(t, model.DB.Create(lane).Error)
	// A 优先级高（先打），B 兜底。
	require.NoError(t, model.DB.Create(&model.LaneMember{
		LaneId: lane.Id, ChannelId: channelA.Id, Model: laneName, Priority: 2,
	}).Error)
	require.NoError(t, model.DB.Create(&model.LaneMember{
		LaneId: lane.Id, ChannelId: channelB.Id, Model: laneName, Priority: 1,
	}).Error)

	engine := gin.New()
	SetPBRRouter(engine)
	SetRelayRouter(engine)
	return engine
}

func chatStreamRequest(laneName string) string {
	body, _ := json.Marshal(map[string]any{
		"model":    laneName,
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	return string(body)
}

func doChatStream(t *testing.T, engine *gin.Engine, laneName string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(chatStreamRequest(laneName)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer free-limit-e2e-key")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

type memberAttempt struct {
	Member  string `json:"member"`
	Status  string `json:"status"`
	ErrKind string `json:"error_kind"`
}

func latestAttempts(t *testing.T) (total int, attempts []memberAttempt) {
	t.Helper()
	var entry model.PBRRequestLog
	require.NoError(t, model.DB.Order("id DESC").First(&entry).Error)
	require.NoError(t, json.Unmarshal([]byte(entry.Attempts), &attempts))
	return entry.TotalAttempts, attempts
}

func TestFreeLimitErrorFailsOverToNextMember(t *testing.T) {
	t.Setenv("PBR_ADMIN_KEY", "free-limit-e2e-admin")
	setupRelayRouterTestDB(t)
	migratePBRMetricsTestTables(t)
	createTestClientKey(t, "free-limit-e2e-key", "free-limit-e2e-key")

	const laneName = "free-limit-lane"
	limitUpstream := fakeupstream.New(fakeupstream.Config{
		Behaviors: map[string]fakeupstream.Behavior{
			laneName: {Mode: "opening_then_free_limit_error"},
		},
	})
	t.Cleanup(limitUpstream.Close)
	healthyUpstream := fakeupstream.New(fakeupstream.Config{})
	t.Cleanup(healthyUpstream.Close)

	engine := newFreeLimitFailoverEngine(t,
		limitUpstream.URL, healthyUpstream.URL, laneName)

	// 第 1 次：A 开场帧 + 流内免费额度错误（未提交）→ 换 B → 客户端拿到正常响应。
	recorder := doChatStream(t, engine, laneName)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := recorder.Body.String()
	assert.Contains(t, body, "pong", "必须由健康成员 B 交付内容")
	assert.NotContains(t, body, "Free usage limit", "上游拒绝不得透传给客户端")
	assert.NotContains(t, body, "chatcmpl-limit", "A 的开场帧（已暂存）不得泄漏给客户端")

	// attempts 链：A 拒绝（报文含免费额度），B 成功。
	total, attempts := latestAttempts(t)
	require.GreaterOrEqual(t, total, 2)
	require.Len(t, attempts, total)
	failed := attempts[0]
	assert.Equal(t, "limit-ch", strings.Split(failed.Member, "/")[0],
		"attempts 按时间序：第一跳是限额成员")
	assert.NotEqual(t, "success", failed.Status)
	last := attempts[len(attempts)-1]
	assert.Equal(t, "healthy-ch", strings.Split(last.Member, "/")[0])
	assert.Equal(t, "success", last.Status)

	// 第 2 次：A 已进 hard_quota 长冷却 → 直接由 B 服务，A 不再被请求。
	before := len(limitUpstream.Requests())
	recorder2 := doChatStream(t, engine, laneName)
	require.Equal(t, http.StatusOK, recorder2.Code, recorder2.Body.String())
	assert.Contains(t, recorder2.Body.String(), "pong")
	after := len(limitUpstream.Requests())
	assert.Equal(t, before, after, "冷却中的 A 不得再被请求")
}

// 只有这条腿时：干净失败（503 No available channel），不是垃圾 200 空流。
func TestFreeLimitErrorAloneReturnsCleanError(t *testing.T) {
	t.Setenv("PBR_ADMIN_KEY", "free-limit-e2e-admin")
	setupRelayRouterTestDB(t)
	migratePBRMetricsTestTables(t)
	createTestClientKey(t, "free-limit-e2e-key", "free-limit-e2e-key")

	const laneName = "free-limit-only-lane"
	limitUpstream := fakeupstream.New(fakeupstream.Config{
		Behaviors: map[string]fakeupstream.Behavior{
			laneName: {Mode: "opening_then_free_limit_error"},
		},
	})
	t.Cleanup(limitUpstream.Close)

	baseA := limitUpstream.URL
	channelA := &model.Channel{
		Name:    "limit-only-ch",
		Type:    constant.ChannelTypeOpenAI,
		Key:     "sk-a",
		BaseURL: &baseA,
		Status:  common.ChannelStatusEnabled,
		Group:   "default",
		Models:  laneName,
	}
	require.NoError(t, model.DB.Create(channelA).Error)
	lane := &model.Lane{Name: laneName, Enabled: true, Mode: model.LaneModeFailover}
	require.NoError(t, model.DB.Create(lane).Error)
	require.NoError(t, model.DB.Create(&model.LaneMember{
		LaneId: lane.Id, ChannelId: channelA.Id, Model: laneName, Priority: 1,
	}).Error)

	engine := gin.New()
	SetPBRRouter(engine)
	SetRelayRouter(engine)

	recorder := doChatStream(t, engine, laneName)

	// 客户端拿干净错误，而不是 HTTP 200 的空流。
	assert.NotEqual(t, http.StatusOK, recorder.Code,
		"零内容流不得以 200 交付，body=%s", recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "chatcmpl-limit",
		"A 的开场帧（已暂存）不得泄漏给客户端")
	assert.NotContains(t, recorder.Body.String(), "Free usage limit",
		"未提交的上游报错不得透传给客户端")

	// 日志记失败，不是成功。
	var entry model.PBRRequestLog
	require.NoError(t, model.DB.Order("id DESC").First(&entry).Error)
	assert.False(t, entry.Success, "零内容流必须记失败")
	assert.Zero(t, entry.CompletionTokens)
}
