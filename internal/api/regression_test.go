package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api-test.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Lane{}, &model.LaneMember{}, &model.ClientKey{}, &model.Model{}, &model.Option{}, &model.User{}, &model.AuditLog{}))
	// 列名常量按数据库类型初始化：未初始化时依赖 commonKeyCol 的 SQL 会语法错误。
	model.InitColumnNames()
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	// 测试隔离：并发用例可能把 RedisEnabled 置真，而测试没有真实 Redis 客户端，
	// 会让 GetUsernameById 走缓存分支并 panic（nil RDB）。审计路径必须走 DB 分支。
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedis })
	return db
}

// dry-run 必须能发现"真实导入会 422"的问题（车道成员引用不存在的渠道），
// 否则调用方拿到 valid:true 却在下一次真实导入失败。
//
// 注意：校验函数只返回错误、不写响应（响应由最外层 handler 写一次，审查 F6），
// 因此这里断言"错误携带的状态与 code"，再由 writeAPIError 验证实际响应。
func TestValidateBundleRejectsMissingMemberChannel(t *testing.T) {
	setupAPITestDB(t)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	bundle := &ConfigBundle{
		Version: "v1",
		Lanes: []LaneConfig{{
			Name: "imp", Enabled: true, Mode: model.LaneModeFailover,
			Members: []LaneMemberConfig{{Channel: "ghost", Model: "x", Priority: 1}},
		}},
	}
	err := validateBundle(c, bundle)
	require.Error(t, err, "引用不存在渠道的车道必须校验失败")
	assert.Zero(t, recorder.Body.Len(), "校验函数不得自己写响应体（否则与 handler 双写）")

	ae, ok := err.(*apiError)
	require.True(t, ok, "校验错误必须携带对外状态与 code")
	assert.Equal(t, http.StatusUnprocessableEntity, ae.status)
	assert.Equal(t, apierr.CodeMemberChannelMissing, ae.code)

	writeAPIError(c, err)
	assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "member_channel_missing")
}

func strPtr(value string) *string { return &value }

// param_override 必须是 JSON 对象；裸字符串也是合法 JSON，但合并时会出错。
func TestBuildChannelRejectsNonObjectParamOverride(t *testing.T) {
	setupAPITestDB(t)
	_, err := buildChannel("po", nil, &channelPayload{
		Type:          strPtr("openai"),
		BaseURL:       strPtr("http://upstream.example"),
		ParamOverride: json.RawMessage("\"{bad\""),
	}, true)
	require.Error(t, err)
	assert.Contains(t, err.message, "param_override must be a json object")
}

// 车道是唯一入口：没有车道的模型没有运行态入口；固化成车道后才能解析（ADR 0005）。
func TestResolveRuntimeRouteRequiresLane(t *testing.T) {
	db := setupAPITestDB(t)
	channel := &model.Channel{Name: "lane-only-ch", Type: 1, Key: "sk-x", Status: common.ChannelStatusEnabled, Group: "default", Models: "lane-only-model"}
	require.NoError(t, db.Create(channel).Error)

	resolvedNoLane, _, err := resolveRuntimeRoute("lane-only-model")
	require.NoError(t, err)
	assert.Nil(t, resolvedNoLane, "没有车道的模型不应有运行态")

	require.NoError(t, model.UpsertLane(&model.Lane{
		Name: "lane-only-model", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: channel.Id, Priority: 10}},
	}))

	resolved, key, err := resolveRuntimeRoute("lane-only-model")
	require.NoError(t, err)
	require.NotNil(t, resolved)
	assert.Equal(t, "lane-only-model", key)
	require.Len(t, resolved.Members, 1)
}
