package api

// 探活语义（ADR 0013 第 5 条、routing-spec §5.4）：200+error 判失败、
// 失败按共享分类、空模型不回落、Azure URL 正确、成功清惩罚态、dry_run 拒绝。

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func probeTestChannel(baseURL string, channelType int) *model.Channel {
	return &model.Channel{
		Name:        "probe-ch",
		Type:        channelType,
		BaseURL:     common.GetPointer(baseURL),
		Key:         "sk-probe-test",
		Status:      common.ChannelStatusEnabled,
		Group:       "default",
		Models:      "model-a",
		CreatedTime: 1_800_000_000,
	}
}

func TestProbeChannelJudgesRealResponse(t *testing.T) {
	var lastPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		switch r.URL.Path {
		case "/ok/v1/chat/completions":
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`)
		case "/blocked/v1/chat/completions":
			_, _ = io.WriteString(w, `{"error":{"message":"blocked by policy","type":"invalid_request_error"}}`)
		case "/auth/v1/chat/completions":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"invalid key"}}`)
		case "/boom/v1/chat/completions":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"upstream exploded"}}`)
		case "/empty/v1/chat/completions":
			_, _ = io.WriteString(w, `{"choices":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"message":"not found"}}`)
		}
	}))
	defer server.Close()

	cases := []struct {
		path     string
		wantOK   bool
		wantKind string
	}{
		{"/ok", true, ""},
		{"/blocked", false, string(route.KindUpstreamReject)},
		{"/auth", false, string(route.KindHardAuth)},
		{"/boom", false, string(route.KindSoftTransient)},
		{"/empty", false, string(route.KindBadResponse)},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			channel := probeTestChannel(server.URL+tc.path, constant.ChannelTypeOpenAI)
			result := probeChannel(t.Context(), channel, "model-a")
			result.finish()
			assert.Equal(t, tc.wantOK, result.OK)
			if tc.wantOK {
				assert.Equal(t, "success", result.Status)
			} else {
				assert.Equal(t, "failed", result.Status)
				assert.Equal(t, tc.wantKind, result.ErrorKind)
			}
			if tc.path == "/ok" {
				assert.Equal(t, "/ok/v1/chat/completions", lastPath)
			}
		})
	}
}

func TestProbeChannelEmptyModelDoesNotFallback(t *testing.T) {
	channel := probeTestChannel("http://127.0.0.1:1", constant.ChannelTypeOpenAI)
	result := probeChannel(t.Context(), channel, "")
	result.finish()
	assert.False(t, result.OK)
	assert.Equal(t, "invalid_request", result.ErrorKind)
	assert.Contains(t, result.Msg, "no model")
}

func TestProbeChannelUnsupportedKindIsExplicit(t *testing.T) {
	channel := probeTestChannel("http://127.0.0.1:1", constant.ChannelTypeAnthropic)
	result := probeChannel(t.Context(), channel, "claude-x")
	result.finish()
	assert.False(t, result.OK)
	assert.Equal(t, "unsupported", result.ErrorKind, "unsupported 是独立 error_kind，不是 network_error")
}

func TestProbeChannelAzureURL(t *testing.T) {
	var gotPath, gotQuery, gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotKey = r.Header.Get("api-key")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"pong"}}]}`)
	}))
	defer server.Close()

	channel := probeTestChannel(server.URL, constant.ChannelTypeAzure)
	channel.Other = "2024-02-01"
	result := probeChannel(t.Context(), channel, "my.deployment")
	result.finish()
	require.True(t, result.OK, result.Msg)
	assert.Equal(t, "/openai/deployments/my.deployment/chat/completions", gotPath)
	assert.Contains(t, gotQuery, "api-version=2024-02-01")
	assert.Equal(t, "sk-probe-test", gotKey)
}

// seedProbePenalty 用真实的选路失败写入一份成员冷却，供"探活清惩罚态"断言。
func seedProbePenalty(t *testing.T, chID int, upstream, lane string) *model.ResolvedRoute {
	t.Helper()
	cfg := model.DefaultLaneRelayConfig()
	cfg.MemberMaxAttempts = 1
	cfg.MemberCooldownSeconds = 60
	resolved := &model.ResolvedRoute{
		Model:  lane,
		Source: model.RouteSourceExplicit,
		Mode:   model.LaneModeFailover,
		Config: cfg,
		Members: []model.RouteMember{{
			ChannelId:      chID,
			Channel:        "probe-ch",
			ChannelEnabled: true,
			Model:          upstream,
			UpstreamModel:  upstream,
			Priority:       100,
		}},
	}
	state := route.NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	_, _, ok = state.Next(types.NewOpenAIError(errors.New("boom"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError))
	require.False(t, ok)
	t.Cleanup(func() { route.Default.For(lane).Reset() })
	return resolved
}

func probeMemberHealth(t *testing.T, resolved *model.ResolvedRoute, lane string) route.MemberHealth {
	t.Helper()
	snapshot := route.Default.For(lane).Health(resolved, route.CurrentCircuitSettings())
	require.Len(t, snapshot.Members, 1)
	return snapshot.Members[0]
}

// 渠道级探活：应用 model_mapping、成功清惩罚态并回传 penalty_cleared；
// ?dry_run=true 必须拒绝且不请求上游。
func TestTestChannelAppliesMappingAndClearsPenalty(t *testing.T) {
	setupAPITestDB(t)

	var hits atomic.Int32
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"pong"}}]}`)
	}))
	defer server.Close()

	mapping := `{"selected-model":"upstream-model"}`
	ch := &model.Channel{
		Name:         "probe-map-ch",
		Type:         constant.ChannelTypeOpenAI,
		BaseURL:      common.GetPointer(server.URL),
		Key:          "sk-probe-test",
		Status:       common.ChannelStatusEnabled,
		Group:        "default",
		Models:       "selected-model",
		ModelMapping: &mapping,
		CreatedTime:  1_800_000_000,
	}
	require.NoError(t, model.DB.Create(ch).Error)

	laneName := "probe-handler-lane"
	resolved := seedProbePenalty(t, ch.Id, "upstream-model", laneName)
	require.NotNil(t, probeMemberHealth(t, resolved, laneName).CooldownUntil, "前置：应有冷却")

	recorder := callAPI(t, http.MethodPost, "/api/v1/channels/probe-map-ch/test?model=selected-model", "",
		TestChannel, gin.Params{{Key: "name", Value: "probe-map-ch"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var result ProbeMember
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	require.True(t, result.OK, result.Msg)
	assert.Equal(t, "upstream-model", result.UpstreamModel, "探活必须使用 model_mapping 后的上游真名")
	assert.Contains(t, gotBody, `"model":"upstream-model"`)
	require.NotNil(t, result.PenaltyCleared)
	assert.Equal(t, 1, result.PenaltyCleared.Members)
	assert.Contains(t, result.PenaltyCleared.Lanes, laneName)
	health := probeMemberHealth(t, resolved, laneName)
	assert.Nil(t, health.CooldownUntil, "成功探活必须清除冷却")
	assert.Nil(t, health.PacedUntil, "成功探活必须清除减速")

	// dry_run 必须在发请求前拒绝。
	before := hits.Load()
	recorder = callAPI(t, http.MethodPost, "/api/v1/channels/probe-map-ch/test?dry_run=true", "",
		TestChannel, gin.Params{{Key: "name", Value: "probe-map-ch"}})
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "dry_run_not_supported")
	assert.Equal(t, before, hits.Load(), "dry_run 不得触发真实上游请求")
}

// 探活错误会回显给管理面并落探活历史：transport URL/上游错误体可能带 key，
// finish 必须统一脱敏（分类在脱敏前完成）。
func TestProbeFinishMasksUpstreamSecrets(t *testing.T) {
	result := ProbeMember{
		Error: `Post "https://vendor.example.test/v1/chat?key=sk-secret123456": dial tcp: connection refused`,
	}
	result.finish()

	assert.NotContains(t, result.Error, "sk-secret123456")
	assert.NotContains(t, result.Msg, "sk-secret123456")
	assert.NotContains(t, result.Error, "vendor.example.test")
	assert.NotContains(t, result.Msg, "vendor.example.test")
	assert.Equal(t, "network_error", result.ErrorKind)
}

// Msg 可能由调用方单独设置（与 Error 不同源），同样必须脱敏。
func TestProbeFinishMasksMessageOnly(t *testing.T) {
	result := ProbeMember{
		Msg:        `upstream said: key=sk-msgsecret123456 at https://vendor.example.test/v1`,
		StatusCode: http.StatusBadRequest,
		ErrorKind:  string(route.KindClientError),
	}
	result.finish()

	assert.NotContains(t, result.Msg, "sk-msgsecret123456")
	assert.NotContains(t, result.Msg, "vendor.example.test")
}

// 探活失败不得洗掉既有惩罚态（routing-spec §5.4）。
func TestTestChannelFailureKeepsPenalty(t *testing.T) {
	setupAPITestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid key"}}`)
	}))
	defer server.Close()

	ch := &model.Channel{
		Name:        "probe-fail-ch",
		Type:        constant.ChannelTypeOpenAI,
		BaseURL:     common.GetPointer(server.URL),
		Key:         "sk-bad",
		Status:      common.ChannelStatusEnabled,
		Group:       "default",
		Models:      "model-a",
		CreatedTime: 1_800_000_000,
	}
	require.NoError(t, model.DB.Create(ch).Error)

	laneName := "probe-fail-lane"
	resolved := seedProbePenalty(t, ch.Id, "model-a", laneName)

	recorder := callAPI(t, http.MethodPost, "/api/v1/channels/probe-fail-ch/test", "",
		TestChannel, gin.Params{{Key: "name", Value: "probe-fail-ch"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var result ProbeMember
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, string(route.KindHardAuth), result.ErrorKind)
	assert.Nil(t, result.PenaltyCleared, "失败探活不得回传罚态清理")
	assert.NotNil(t, probeMemberHealth(t, resolved, laneName).CooldownUntil, "失败探活不得清除既有冷却")
}
