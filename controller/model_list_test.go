package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/middleware"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"gorm.io/gorm"
)

type listModelsResponse struct {
	Success bool               `json:"success"`
	Data    []dto.OpenAIModels `json:"data"`
	Object  string             `json:"object"`
}

func setupModelListControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	initModelListColumnNames(t)

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Channel{}, &model.Model{},
		&model.Lane{}, &model.LaneMember{}, &model.ClientKey{},
	))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func initModelListColumnNames(t *testing.T) {
	t.Helper()

	originalIsMasterNode := common.IsMasterNode
	originalSQLitePath := common.SQLitePath
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")
	defer func() {
		common.IsMasterNode = originalIsMasterNode
		common.SQLitePath = originalSQLitePath
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	}()

	common.IsMasterNode = false
	common.SQLitePath = fmt.Sprintf("file:%s_init?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))

	require.NoError(t, model.InitDB())
	if model.DB != nil {
		sqlDB, err := model.DB.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
}

// seedLane 建一条车道（含一个成员渠道），返回车道名。
func seedLane(t *testing.T, db *gorm.DB, name string, mode string, enabled bool, memberChannelId int) {
	t.Helper()
	lane := &model.Lane{Name: name, Mode: mode, Enabled: enabled}
	require.NoError(t, db.Create(lane).Error)
	if memberChannelId > 0 {
		require.NoError(t, db.Create(&model.LaneMember{
			LaneId: lane.Id, ChannelId: memberChannelId, UpstreamModel: name, Priority: 10,
		}).Error)
	}
}

func seedChannel(t *testing.T, db *gorm.DB, id int, name, models string) {
	t.Helper()
	require.NoError(t, db.Create(&model.Channel{
		Id: id, Name: name, Type: constant.ChannelTypeOpenAI, Key: "sk-test",
		Status: common.ChannelStatusEnabled, Group: "default", Models: models,
	}).Error)
}

// listModelsWithKey 走真实鉴权上下文装配（SetupContextForPBRClientKey），
// 再调用 ListModels 并返回响应体的模型 id 集合。
func listModelsWithKey(t *testing.T, key *model.ClientKey) map[string]struct{} {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	middleware.SetupContextForPBRClientKey(ctx, key)

	ListModels(ctx, constant.ChannelTypeOpenAI)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload listModelsResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, "list", payload.Object)
	require.NotNil(t, payload.Data, "data 必须是数组而不是 null")

	ids := make(map[string]struct{}, len(payload.Data))
	for _, item := range payload.Data {
		require.Equal(t, "model", item.Object)
		ids[item.Id] = struct{}{}
	}
	return ids
}

func decodeListModelsPayload(t *testing.T, recorder *httptest.ResponseRecorder) listModelsResponse {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload listModelsResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, "list", payload.Object)
	return payload
}

// design-v1 §4：GET /v1/models 按车道枚举——LanePolicy 为"全部车道"时枚举全部
// 启用车道成员声明的模型（路由键=车道名），不再按 group/abilities 聚合。
func TestListModelsAllLanePolicyEnumeratesEnabledLanes(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	seedChannel(t, db, 11, "chan-1", "lane-a,lane-b,lane-nolane")
	seedLane(t, db, "lane-a", model.LaneModeFailover, true, 11)
	seedLane(t, db, "lane-b", model.LaneModeFailover, true, 11)
	// 停用车道与空成员车道不可调用（ResolveRoute 空链 → 503），不得出现在清单里。
	seedLane(t, db, "lane-disabled", model.LaneModeFailover, false, 11)
	seedLane(t, db, "lane-empty", model.LaneModeFailover, true, 0)

	ids := listModelsWithKey(t, &model.ClientKey{Name: "all-key", LanePolicy: ""})
	require.Equal(t, map[string]struct{}{"lane-a": {}, "lane-b": {}}, ids)
}

// 单车道密钥只见本车道模型（LanePolicy mode=allow 白名单）。
func TestListModelsSingleLaneKeySeesOnlyItsLane(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	seedChannel(t, db, 21, "chan-21", "lane-x")
	seedChannel(t, db, 22, "chan-22", "lane-y")
	seedLane(t, db, "lane-x", model.LaneModeFailover, true, 21)
	seedLane(t, db, "lane-y", model.LaneModeFailover, true, 22)

	key := &model.ClientKey{
		Name:       "lane-x-only",
		LanePolicy: `{"mode":"allow","allow_lanes":["lane-x"],"deny_lanes":[]}`,
	}
	ids := listModelsWithKey(t, key)
	require.Equal(t, map[string]struct{}{"lane-x": {}}, ids)
}

// LanePolicy 白名单裁剪：allow 集内的车道可见，deny 优先于 allow 与 all。
func TestListModelsLanePolicyDenyTrimsEvenAllMode(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	seedChannel(t, db, 31, "chan-31", "lane-keep,lane-drop")
	seedLane(t, db, "lane-keep", model.LaneModeFailover, true, 31)
	seedLane(t, db, "lane-drop", model.LaneModeFailover, true, 31)

	denyKey := &model.ClientKey{
		Name:       "deny-key",
		LanePolicy: `{"mode":"all","deny_lanes":["lane-drop"]}`,
	}
	ids := listModelsWithKey(t, denyKey)
	require.Equal(t, map[string]struct{}{"lane-keep": {}}, ids)

	// allow 模式下 deny 同样优先：allow 命中 + deny 命中 → 不可见。
	dualKey := &model.ClientKey{
		Name:       "dual-key",
		LanePolicy: `{"mode":"allow","allow_lanes":["lane-keep","lane-drop"],"deny_lanes":["lane-drop"]}`,
	}
	ids = listModelsWithKey(t, dualKey)
	require.Equal(t, map[string]struct{}{"lane-keep": {}}, ids)
}

// 无策略命中返回空 data 而非回退 group（渠道仍声明着 lane-secret）。
func TestListModelsNoPolicyMatchReturnsEmptyData(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	seedChannel(t, db, 41, "chan-41", "lane-secret")
	seedLane(t, db, "lane-secret", model.LaneModeFailover, true, 41)

	key := &model.ClientKey{
		Name:       "empty-allow",
		LanePolicy: `{"mode":"allow","allow_lanes":[]}`,
	}
	ids := listModelsWithKey(t, key)
	require.Empty(t, ids)
}

// manual 车道同样是车道名的唯一路由入口且可调用（routing-spec §2.1），计入清单。
func TestListModelsIncludesManualLane(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	seedChannel(t, db, 51, "chan-51", "lane-manual")
	seedLane(t, db, "lane-manual", model.LaneModeManual, true, 51)

	ids := listModelsWithKey(t, &model.ClientKey{Name: "manual-key"})
	require.Equal(t, map[string]struct{}{"lane-manual": {}}, ids)
}

// 模型面只认 PBR 客户端密钥（W7）：上下文没有密钥时返回空清单，
// 不得回退到旧的 group/abilities 枚举。
func TestListModelsWithoutClientKeyReturnsEmpty(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	seedChannel(t, db, 61, "chan-61", "group-only-model")
	seedLane(t, db, "group-only-model", model.LaneModeFailover, true, 61)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	// 旧语义的上下文残留：分组 default 下有渠道声明 group-only-model。
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")

	ListModels(ctx, constant.ChannelTypeOpenAI)
	payload := decodeListModelsPayload(t, recorder)
	require.Empty(t, payload.Data)
}

// owned_by 取车道首选成员（priority 最大者）的渠道类型适配器名。
func TestListModelsOwnedByPrefersTopPriorityMemberChannel(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	seedChannel(t, db, 71, "chan-71", "lane-owner")
	require.NoError(t, db.Create(&model.Channel{
		Id: 72, Name: "chan-72", Type: constant.ChannelTypeAnthropic, Key: "sk-anthropic",
		Status: common.ChannelStatusEnabled, Group: "default", Models: "lane-owner",
	}).Error)
	lane := &model.Lane{Name: "lane-owner", Mode: model.LaneModeFailover, Enabled: true}
	require.NoError(t, db.Create(lane).Error)
	require.NoError(t, db.Create(&[]model.LaneMember{
		{LaneId: lane.Id, ChannelId: 71, UpstreamModel: "lane-owner", Priority: 1},
		{LaneId: lane.Id, ChannelId: 72, UpstreamModel: "lane-owner", Priority: 100},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	middleware.SetupContextForPBRClientKey(ctx, &model.ClientKey{Name: "owner-key"})
	ListModels(ctx, constant.ChannelTypeOpenAI)

	payload := decodeListModelsPayload(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "lane-owner", payload.Data[0].Id)
	require.Equal(t, "claude", payload.Data[0].OwnedBy)
}

// 车道名仍是 OpenAI 清单里的模型名；端点类型来自模型元数据/定价缓存的口径不变。
func TestListModelsUsesAdvancedCustomEndpointTypesFromPricingCache(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		model.InvalidatePricingCache()
	})

	require.NoError(t, db.Create(&model.User{
		Id:       1003,
		Username: "advanced-custom-model-list-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)

	channel := &model.Channel{
		Id:     701,
		Type:   constant.ChannelTypeAdvancedCustom,
		Key:    "advanced-custom-key",
		Status: common.ChannelStatusEnabled,
		Name:   "advanced-custom-channel",
		Group:  "default",
		Models: "gemini-3.5-flash",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{
			Routes: []dto.AdvancedCustomRoute{
				{
					IncomingPath: "/v1/chat/completions",
					UpstreamPath: "/v1/chat/completions",
				},
				{
					IncomingPath: "/v1/responses",
					UpstreamPath: "/v1beta/models/{model}:generateContent",
					Converter:    "openai_responses_to_gemini_generate_content",
					Models:       []string{"re:^gemini-"},
				},
			},
		},
	})
	require.NoError(t, db.Create(channel).Error)
	seedLane(t, db, "gemini-3.5-flash", model.LaneModeFailover, true, 701)

	model.InitChannelCache()
	model.GetPricing()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx.Set("id", 1003)
	middleware.SetupContextForPBRClientKey(ctx, &model.ClientKey{Name: "advanced-custom-key-ref"})

	ListModels(ctx, constant.ChannelTypeOpenAI)

	payload := decodeListModelsPayload(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "gemini-3.5-flash", payload.Data[0].Id)
	require.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
	}, payload.Data[0].SupportedEndpointTypes)
}
