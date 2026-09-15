package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"pbr/common"
	"pbr/model"

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
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Lane{}, &model.LaneMember{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	return db
}

// dry-run 必须能发现"真实导入会 422"的问题（车道成员引用不存在的渠道），
// 否则调用方拿到 valid:true 却在下一次真实导入失败。
func TestValidateBundleRejectsMissingMemberChannel(t *testing.T) {
	setupAPITestDB(t)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	bundle := &ConfigBundle{
		Version: "v1",
		Lanes: []LaneConfig{{
			Name: "imp", Enabled: true, Mode: model.LaneModeFailover,
			Members: []LaneMemberConfig{{Channel: "ghost", UpstreamModel: "x", Priority: 1}},
		}},
	}
	err := validateBundle(c, bundle)
	require.Error(t, err, "引用不存在渠道的车道必须校验失败")
	assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "member_channel_missing")
}

func strPtr(value string) *string { return &value }

// 负权重会被 uint 回绕成巨大值、落库成负数，再读回 *uint 时报 scan error，
// 从而污染整张渠道缓存并让所有路由 500。必须在写库前拒绝。
func TestBuildChannelRejectsNegativeWeight(t *testing.T) {
	setupAPITestDB(t)
	negative := -1
	_, err := buildChannel("neg", nil, &channelPayload{
		Type: strPtr("openai"), BaseURL: strPtr("http://upstream.example"), Weight: &negative,
	}, true)
	require.Error(t, err)
	assert.Contains(t, err.message, "weight must be between")
}

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

// 隐式的"模型名即路由键"也必须能解析运行态，否则最常用的路由没有任何健康/探活入口。
func TestResolveRuntimeRouteAcceptsImplicitModel(t *testing.T) {
	db := setupAPITestDB(t)
	channel := &model.Channel{Name: "implicit-ch", Type: 1, Key: "sk-x", Status: common.ChannelStatusEnabled, Group: "default", Models: "implicit-model"}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

	resolved, key, err := resolveRuntimeRoute("implicit-model")
	require.NoError(t, err)
	require.NotNil(t, resolved, "隐式模型应能解析出运行态")
	assert.Equal(t, "implicit-model", key)
	require.Len(t, resolved.Members, 1)
}
