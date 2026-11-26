package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/api"
	"github.com/zzyyyds88/PowerBarRations/internal/metrics"
	"github.com/zzyyyds88/PowerBarRations/internal/testutil/fakeupstream"
	"github.com/zzyyyds88/PowerBarRations/model"
)

// G5 入站端点：/v1/responses 与 chat 共用车道语义（design-v1 §4）。
// 路由存在性单独锁住，避免有人把注册行删掉后只剩 handler 单测仍全绿。
func TestRelayRouterRegistersV1ModelEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/v1/responses"},
		{http.MethodPost, "/v1/messages/count_tokens"},
		{http.MethodPost, "/v1/images/variations"},
		{http.MethodPost, "/v1/moderations"},
	} {
		handler, ok := registeredHandler(engine, route.method, route.path)
		require.True(t, ok, "%s %s 未注册", route.method, route.path)
		assert.NotContains(t, handler, "RelayNotImplemented", "%s %s 不得再是 501 占位", route.method, route.path)
	}
}

// /v1/responses 走 PBR 车道；无车道时必须是 503 + 固定 body（§4.1），
// 流式与非流式同形——下游 fallback 分类依赖它，不能变成 400/404。
func TestResponsesRouteReturnsNoAvailableChannel503(t *testing.T) {
	setupRelayRouterTestDB(t)
	createTestClientKey(t, "responses-key", "responses-test-key")

	engine := gin.New()
	SetRelayRouter(engine)

	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			body := fmt.Sprintf(`{"model":"missing-lane","input":"hello","stream":%t}`, stream)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer responses-test-key")

			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
			assert.Contains(t, recorder.Body.String(), "No available channel for model missing-lane")
		})
	}
}

// /v1/messages/count_tokens 本地计算：鉴权、正常返回、错误请求三条路径。
func TestCountTokensRoute(t *testing.T) {
	setupRelayRouterTestDB(t)
	createTestClientKey(t, "count-key", "count-test-key")

	engine := gin.New()
	SetRelayRouter(engine)

	validBody := `{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hello world"}]}`

	t.Run("missing client key is rejected", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(validBody))
		request.Header.Set("Content-Type", "application/json")

		engine.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	})

	t.Run("valid request returns input_tokens", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(validBody))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer count-test-key")

		engine.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		var payload map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
		tokens, ok := payload["input_tokens"].(float64)
		require.True(t, ok, recorder.Body.String())
		assert.Greater(t, tokens, float64(0))
	})

	t.Run("invalid request returns 400 error envelope", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens",
			strings.NewReader(`{"model":"claude-3-5-sonnet"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer count-test-key")

		engine.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		var payload map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
		assert.Equal(t, "error", payload["type"])
	})
}

// /v1/images/variations 不再 501，且 multipart 表单里的 model 必须成为路由键
// （middleware/distributor.go 的图像上传分支）：无车道时应是 503 而不是
// "模型名必填" 400，也不是 501。
func TestImagesVariationsRouteUsesMultipartModel(t *testing.T) {
	setupRelayRouterTestDB(t)
	createTestClientKey(t, "variations-key", "variations-test-key")

	engine := gin.New()
	SetRelayRouter(engine)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "missing-variation-lane"))
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("fake image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/images/variations", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer variations-test-key")

	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "No available channel for model missing-variation-lane")
}

// moderations 端到端：独立 relay mode 透传 moderation 对象，且收尾指标只增
// requests_total（不混入 chat 的 token/usage 语义）。
func TestModerationsRoutePassesThroughAndRecordsMetrics(t *testing.T) {
	t.Setenv("PBR_ADMIN_KEY", "router-metrics-admin-key")
	setupRelayRouterTestDB(t)
	migratePBRMetricsTestTables(t)
	createTestClientKey(t, "moderations-key", "moderations-test-key")

	const moderationBody = `{"id":"modr-1","model":"upstream-reported-name","results":[{"flagged":true,"categories":{"hate":true},"category_scores":{"hate":0.93}}]}`
	upstream := fakeupstream.New(fakeupstream.Config{
		Behaviors: map[string]fakeupstream.Behavior{
			"mod-lane": {Body: moderationBody},
		},
	})
	t.Cleanup(upstream.Close)

	baseURL := upstream.URL
	channel := &model.Channel{
		Name:    "mod-ch",
		Type:    constant.ChannelTypeOpenAI,
		Key:     "sk-test",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
		Group:   "default",
		Models:  "mod-lane",
	}
	require.NoError(t, model.DB.Create(channel).Error)
	lane := &model.Lane{Name: "mod-lane", Enabled: true, Mode: model.LaneModeFailover}
	require.NoError(t, model.DB.Create(lane).Error)
	require.NoError(t, model.DB.Create(&model.LaneMember{
		LaneId: lane.Id, ChannelId: channel.Id, Model: "mod-lane", Priority: 1,
	}).Error)

	engine := gin.New()
	SetPBRRouter(engine)
	SetRelayRouter(engine)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/moderations",
		strings.NewReader(`{"model":"mod-lane","input":"some text"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer moderations-test-key")
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	// §4.1：model 回填请求名（车道名），上游真名不外泄；moderation 字段完整保留。
	assert.Equal(t, "mod-lane", response["model"])
	results, ok := response["results"].([]any)
	require.True(t, ok, recorder.Body.String())
	require.Len(t, results, 1)
	assert.Equal(t, true, results[0].(map[string]any)["flagged"])

	last, ok := upstream.LastRequest()
	require.True(t, ok, "假上游必须真的收到请求")
	assert.Equal(t, "/v1/moderations", last.Path)

	// 抓取时只接进程内计数器（G3 契约）：收尾点必须真的写入了这次请求。
	metricsRecorder := httptest.NewRecorder()
	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRequest.Header.Set("Authorization", "Bearer router-metrics-admin-key")
	engine.ServeHTTP(metricsRecorder, metricsRequest)
	require.Equal(t, http.StatusOK, metricsRecorder.Code, metricsRecorder.Body.String())
	text := metricsRecorder.Body.String()
	assert.Contains(t, text, `pbr_requests_total{channel="mod-ch",lane="mod-lane",model="mod-lane",status="200"}`)
	assert.NotContains(t, text, `pbr_tokens_total{kind="prompt",model="mod-lane"}`,
		"moderations 不得混入 chat token 用量语义")
	assert.NotContains(t, text, `pbr_cost_total{channel="mod-ch",model="mod-lane"}`,
		"moderations 不产生折算成本")
}

// capabilities 必须与**实际注册**一致（api-spec §5.1）：
// 声明 true 的功能要有路由，声明 false 的功能不得已有可用实现。
// G6 接上 Files/Batches 时，这个守卫会强制同步 capabilities。
func TestCapabilitiesMatchRegisteredModelRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	relayEngine := gin.New()
	SetRelayRouter(relayEngine)
	pbrEngine := gin.New()
	SetPBRRouter(pbrEngine)

	capsEngine := gin.New()
	capsEngine.GET("/capabilities", api.GetCapabilities)
	recorder := httptest.NewRecorder()
	capsEngine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	// 既有键保持向后兼容（只增不删）。
	for _, key := range []string{"api_version", "lane_modes", "adapters", "inbound_formats", "circuit"} {
		assert.Contains(t, payload, key)
	}
	features, ok := payload["features"].(map[string]any)
	require.True(t, ok, "capabilities.features 缺失：%s", recorder.Body.String())

	featureRoute := map[string]struct{ method, path string }{
		"responses":         {http.MethodPost, "/v1/responses"},
		"count_tokens":      {http.MethodPost, "/v1/messages/count_tokens"},
		"images_variations": {http.MethodPost, "/v1/images/variations"},
		"moderations":       {http.MethodPost, "/v1/moderations"},
	}
	for feature, route := range featureRoute {
		_, registered := registeredHandler(relayEngine, route.method, route.path)
		assert.Equal(t, registered, features[feature],
			"capabilities.features.%s 与实际注册不一致（%s %s）", feature, route.method, route.path)
	}

	assert.Equal(t, hasFunctionalRoute(relayEngine, "/v1/files"), features["files"],
		"capabilities.features.files 必须反映 Files 是否已真实可用")
	assert.Equal(t, hasFunctionalRoute(relayEngine, "/v1/batches"), features["batches"],
		"capabilities.features.batches 必须反映 Batches 是否已真实可用")

	_, metricsRegistered := registeredHandler(pbrEngine, http.MethodGet, "/metrics")
	assert.Equal(t, metricsRegistered, features["metrics"], "capabilities.features.metrics 与 /metrics 注册不一致")

	asyncObjects, ok := payload["async_objects"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "X-PBR-Lane", asyncObjects["lane_header"])
	assert.Equal(t, features["files"], asyncObjects["files"])
	assert.Equal(t, features["batches"], asyncObjects["batches"])

	metricCaps, ok := payload["metrics"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "/metrics", metricCaps["path"])
}

// /api/logs/export 必须挂在管理面 authed 组，并自动覆盖两个前缀。
func TestLogsExportRouteIsRegisteredUnderBothPrefixes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetPBRRouter(engine)

	for _, path := range []string{"/api/logs/export", "/api/v1/logs/export"} {
		handler, ok := registeredHandler(engine, http.MethodGet, path)
		require.True(t, ok, "GET %s 未注册", path)
		assert.Contains(t, handler, "api.ExportLogs")
	}
}

// /metrics 复用管理密钥 Bearer（token-spec §2）：无/错密钥 401，正确密钥 200 且
// 输出 Prometheus 文本。
func TestMetricsEndpointRequiresAdminKey(t *testing.T) {
	t.Setenv("PBR_ADMIN_KEY", "router-metrics-endpoint-key")
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetPBRRouter(engine)
	require.NotPanics(t, func() { engine.Routes() }) // 与 /api 路由共存无冲突

	// 预热一条序列：CounterVec 在首次使用前不会出现在文本输出里。
	metrics.ObserveRequest(metrics.RequestObservation{Model: "metrics-model", Lane: "metrics-lane", Channel: "metrics-channel", Status: http.StatusOK})

	t.Run("missing bearer is rejected", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	})

	t.Run("valid bearer returns prometheus text", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		request.Header.Set("Authorization", "Bearer router-metrics-endpoint-key")
		engine.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		assert.Contains(t, recorder.Header().Get("Content-Type"), "text/plain")
		text := recorder.Body.String()
		assert.Contains(t, text, "pbr_requests_total")
		assert.Contains(t, text, `channel="metrics-channel"`)
		assert.Contains(t, text, "go_goroutines", "Go runtime 默认指标必须保留")
	})
}

// registeredHandler 返回某方法+路径的实际末端 handler 名。
func registeredHandler(engine *gin.Engine, method, path string) (string, bool) {
	for _, info := range engine.Routes() {
		if info.Method == method && info.Path == path {
			return info.Handler, true
		}
	}
	return "", false
}

// hasFunctionalRoute 判断某前缀下是否存在**非 501 占位**的路由。
// Files/Batches 现在以 RelayNotImplemented 注册，不能算"能力已上线"。
func hasFunctionalRoute(engine *gin.Engine, prefix string) bool {
	for _, info := range engine.Routes() {
		if strings.HasPrefix(info.Path, prefix) && !strings.Contains(info.Handler, "RelayNotImplemented") {
			return true
		}
	}
	return false
}

// migratePBRMetricsTestTables 让收尾点的日志写入在测试库里真实执行。
func migratePBRMetricsTestTables(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(
		&model.PBRRequestLog{},
		&model.PBRRequestContext{},
		&model.PBRStatsHourly{},
		&model.PBRStatsOutbox{},
	))
}
