package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 导出「新渠道 + 引用它的车道」导入空实例时，成员不能被误判悬空而跳过：
// 构造车道发生在渠道落库之前，剪枝必须把本 bundle 声明的渠道算作存在。
func TestImportKeepsMembersOfChannelsDeclaredInSameBundle(t *testing.T) {
	setupAPITestDB(t)
	bundle := &ConfigBundle{
		Version:  "v1",
		Channels: []ChannelConfig{{Name: "new-ch", Type: "openai", Enabled: true, Models: []string{"m"}}},
		Lanes: []LaneConfig{{
			Name: "m", Enabled: true, Mode: model.LaneModeFailover,
			Members: []LaneMemberConfig{{Channel: "new-ch", Model: "m", Priority: 1}},
		}},
	}
	result := ImportResult{Valid: true, Diff: map[string]any{"lanes": newDiffList()}}

	pruneMissingMemberChannels(bundle, &result)

	require.Len(t, bundle.Lanes[0].Members, 1, "bundle 内声明的渠道不得被判悬空")
	assert.True(t, bundle.Lanes[0].Enabled)
	assert.Empty(t, result.Warnings)
}

// 端到端：真实 PostImport 能把「新渠道 + 新车道」一次导入成功（跨实例还原）。
func TestPostImportRestoresChannelAndLaneInOneBundle(t *testing.T) {
	db := setupAPITestDB(t)
	gin.SetMode(gin.TestMode)
	body := `{"version":"v1","channels":[{"name":"restore-ch","type":"openai","base_url":"http://upstream.example","key":"__INJECT__","enabled":true,"models":["restore-model"]}],` +
		`"lanes":[{"name":"restore-model","enabled":true,"mode":"failover","config":{},"members":[{"channel":"restore-ch","model":"restore-model","priority":1}]}]}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/import", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	PostImport(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp ImportResult
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.Empty(t, resp.Warnings, "跨实例还原不应产生悬空成员警告")

	var ch model.Channel
	require.NoError(t, db.Where("name = ?", "restore-ch").First(&ch).Error)
	lane, err := model.GetLaneByName("restore-model")
	require.NoError(t, err)
	require.Len(t, lane.Members, 1)
	assert.Equal(t, ch.Id, lane.Members[0].ChannelId, "成员必须指向刚导入的渠道")
}

// deny_lanes 与 allow_lanes 对称校验：拼错的键等于没有拒绝。
func TestClientKeyDenyLanesRejectsUnknownKeys(t *testing.T) {
	setupAPITestDB(t)
	payload := &clientKeyPayload{
		LanePolicy: &lanePolicyPayload{Mode: model.LanePolicyModeAll, DenyLanes: []string{"ghost-model"}},
	}
	err := applyClientKeyPayload(&model.ClientKey{Name: "k"}, payload)
	require.NotNil(t, err)
	assert.Equal(t, "validation_failed", err.code)
	assert.Contains(t, err.message, "deny_lanes")
}

// mode 非法时写入必须报错（读取路径仍宽容回落，见 model.ParseLanePolicy）。
func TestClientKeyRejectsInvalidLanePolicyMode(t *testing.T) {
	setupAPITestDB(t)
	payload := &clientKeyPayload{
		LanePolicy: &lanePolicyPayload{Mode: "bogus"},
	}
	err := applyClientKeyPayload(&model.ClientKey{Name: "k"}, payload)
	require.NotNil(t, err)
	assert.Equal(t, "validation_failed", err.code)
	assert.Contains(t, err.message, "mode")
}

// 只配 model_mapping（models 清单不含该键）的渠道也算候选成员（与模型页口径一致）。
func TestSuggestedMembersIncludesMappingOnlyChannel(t *testing.T) {
	db := setupAPITestDB(t)
	mapping := `{"mapped-model":"vendor/real"}`
	ch := &model.Channel{Name: "mapping-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "other-model", ModelMapping: &mapping}
	require.NoError(t, db.Create(ch).Error)

	members, err := model.SuggestedMembers("mapped-model")
	require.NoError(t, err)
	require.Len(t, members, 1, "只在 model_mapping 里映射的渠道应作为候选")
	assert.Equal(t, "mapping-ch", members[0].Channel)
}
