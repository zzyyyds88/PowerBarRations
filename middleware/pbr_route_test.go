package middleware

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/internal/route"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// X-Served-By 只描述"实际成功服务的成员"：失败/503 收尾必须清除，
// 否则下游会把重试链里最后选中的成员误当成服务者（design-v1 §4.1）。
func TestClearServedByHeaderRemovesHeaderAndContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	SetServedByHeader(c, &model.Channel{Id: 7, Name: "channel-a"}, &model.RouteMember{UpstreamModel: "model-x"})
	require.Equal(t, "channel=7:channel-a, model=model-x", c.Writer.Header().Get("X-Served-By"))
	_, hasContext := common.GetContextKey(c, constant.ContextKeyPBRServedBy)
	require.True(t, hasContext, "前提：选路已写入上下文记录")

	ClearServedByHeader(c)
	assert.Empty(t, c.Writer.Header().Get("X-Served-By"), "失败收尾必须删除响应头")
	_, hasContext = common.GetContextKey(c, constant.ContextKeyPBRServedBy)
	assert.False(t, hasContext, "上下文记录也应清除")

	// 幂等：未设置时清理不 panic。
	ClearServedByHeader(c)
	ClearServedByHeader(nil)
}

func usePBRRouteTestDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	require.NoError(t, db.AutoMigrate(
		&model.Channel{}, &model.Lane{}, &model.LaneMember{},
		&model.MemberGroup{}, &model.MemberGroupItem{},
		&model.User{}, &model.ClientKey{}, &model.PBRRequestLog{}, &model.PBRRequestContext{},
	))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
	})
}

func pbrRouteTestContext(t *testing.T, modelName string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"` + modelName + `"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func countPBRQueries(t *testing.T) *int {
	t.Helper()
	count := 0
	const callbackName = "test:count_pbr_queries"
	require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(tx.Statement.SQL.String())), "SELECT") {
			count++
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Query().Remove(callbackName) })
	return &count
}

func TestPBRServeResolvesRouteOnceAndReleasesTicket(t *testing.T) {
	usePBRRouteTestDB(t)
	setting := `{"pbr_rate_limits":[{"model":"model-a","max_concurrency":1,"queue_limit":2,"queue_wait_seconds":1}]}`
	channel := &model.Channel{Id: 1, Name: "channel-a", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
		BaseURL: common.GetPointer("https://example.invalid/v1"), Key: "test-key", Models: "model-a", Setting: &setting}
	require.NoError(t, model.DB.Create(channel).Error)
	lane := &model.Lane{Id: 1, Name: "lane-a", Mode: model.LaneModeFailover, Enabled: true,
		Members: []model.LaneMember{{ChannelId: channel.Id, Model: "model-a"}}}
	require.NoError(t, model.DB.Create(lane).Error)
	t.Cleanup(func() { route.Default.Remove(lane.Name) })

	c, recorder := pbrRouteTestContext(t, lane.Name)
	queries := countPBRQueries(t)
	require.True(t, PBRServe(c))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, 3, *queries, "PBRServe 只应解析一次：车道、成员、渠道各一次查询")

	state := route.From(c)
	require.NotNil(t, state)
	require.NotNil(t, state.Route.Members[0].RateLimit, "route snapshot must carry rate limit")
	key := route.AdmissionKeyOf(&state.Route.Members[0])
	cfg := route.RateLimitConfig{Mode: route.AdmissionModeFixed, MaxConcurrency: 1, QueueLimit: 2, QueueWaitSeconds: 1}
	assert.Equal(t, 1, route.DefaultAdmission.Stats(key, cfg).Inflight)
	ReleasePBRRequestResources(c)
	ReleasePBRRequestResources(c)
	assert.Equal(t, 0, route.DefaultAdmission.Stats(key, cfg).Inflight, "重复释放必须幂等")
}

func TestPBRNextChannelUsesSnapshotWithoutDatabase(t *testing.T) {
	usePBRRouteTestDB(t)
	channel := &model.Channel{Id: 1, Name: "channel-a", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
		BaseURL: common.GetPointer("https://example.invalid/v1"), Key: "test-key", Models: "model-a"}
	require.NoError(t, model.DB.Create(channel).Error)
	lane := &model.Lane{Id: 1, Name: "lane-a", Mode: model.LaneModeFailover, Enabled: true,
		Members: []model.LaneMember{{ChannelId: channel.Id, Model: "model-a"}}}
	require.NoError(t, model.DB.Create(lane).Error)
	t.Cleanup(func() { route.Default.Remove(lane.Name) })

	resolved, err := model.ResolveRoute(lane.Name)
	require.NoError(t, err)
	state := route.NewState(resolved)
	t.Cleanup(state.Close)
	c, _ := pbrRouteTestContext(t, lane.Name)
	queries := countPBRQueries(t)
	selected, nextErr, ok := PBRNextChannel(c, state, lane.Name, nil)
	require.True(t, ok)
	require.Nil(t, nextErr)
	assert.Same(t, resolved.Members[0].ChannelSnapshot, selected)
	assert.Zero(t, *queries, "PBRNextChannel 请求路径不得查库")
}

// 429 的 Retry-After 超上限时，中间件必须把原错误交回 controller
// （由 controller 设 Retry-After 并原样响应），不能合成 503 或继续选路。
func TestPBRNextChannelPropagatesOversizedRetryAfterAbort(t *testing.T) {
	usePBRRouteTestDB(t)
	channel := &model.Channel{Id: 1, Name: "channel-a", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
		BaseURL: common.GetPointer("https://example.invalid/v1"), Key: "test-key", Models: "model-a"}
	require.NoError(t, model.DB.Create(channel).Error)
	lane := &model.Lane{Id: 1, Name: "lane-429-abort", Mode: model.LaneModeFailover, Enabled: true,
		Members: []model.LaneMember{{ChannelId: channel.Id, Model: "model-a"}}}
	require.NoError(t, model.DB.Create(lane).Error)
	t.Cleanup(func() { route.Default.Remove(lane.Name) })

	resolved, err := model.ResolveRoute(lane.Name)
	require.NoError(t, err)
	state := route.NewState(resolved)
	t.Cleanup(state.Close)
	c, _ := pbrRouteTestContext(t, lane.Name)
	selected, nextErr, ok := PBRNextChannel(c, state, lane.Name, nil)
	require.True(t, ok)
	require.NotNil(t, selected)

	rateErr := types.NewOpenAIError(errors.New("rate limited"), types.ErrorCode(""), http.StatusTooManyRequests)
	rateErr.RetryAfterSeconds = 61 // 超过本地 60s 上限
	selected, nextErr, ok = PBRNextChannel(c, state, lane.Name, rateErr)
	assert.False(t, ok)
	assert.Nil(t, selected)
	assert.Same(t, rateErr, nextErr, "必须原样透传上游 429，而不是合成 503")
}

func TestPBRNextChannelFailsClosedOnBadChannelConfig(t *testing.T) {
	usePBRRouteTestDB(t)
	bad := `{"pbr_rate_limits":`
	channel := &model.Channel{Id: 1, Name: "channel-a", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
		BaseURL: common.GetPointer("https://example.invalid/v1"), Key: "test-key", Models: "model-a", Setting: &bad}
	require.NoError(t, model.DB.Create(channel).Error)
	lane := &model.Lane{Id: 1, Name: "lane-a", Mode: model.LaneModeFailover, Enabled: true,
		Members: []model.LaneMember{{ChannelId: channel.Id, Model: "model-a"}}}
	require.NoError(t, model.DB.Create(lane).Error)
	t.Cleanup(func() { route.Default.Remove(lane.Name) })

	resolved, err := model.ResolveRoute(lane.Name)
	require.NoError(t, err)
	require.NotNil(t, resolved.Members[0].ConfigError)
	state := route.NewState(resolved)
	t.Cleanup(state.Close)
	c, _ := pbrRouteTestContext(t, lane.Name)
	queries := countPBRQueries(t)
	selected, nextErr, ok := PBRNextChannel(c, state, lane.Name, nil)
	assert.False(t, ok)
	assert.Nil(t, selected)
	assert.Nil(t, nextErr)
	assert.Zero(t, *queries)
	attempts := state.LogAttempts()
	require.NotEmpty(t, attempts)
	assert.True(t, strings.Contains(attempts[0].Msg, "config error") || strings.Contains(attempts[0].Status, "config"))
}
