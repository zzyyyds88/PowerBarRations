package api

// Phase 5 管理面口径：成员计数互斥分解、reset 结构化响应、日志 error_kind
// 过滤与非法 success 值拒绝。

import (
	"encoding/json"
	"errors"
	"net/http"
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

// 计数互斥：available + disabled + channel_disabled + orphan = member_count。
func TestModelSummariesDisjointMemberCounts(t *testing.T) {
	db := setupAPITestDB(t)

	enabledCh := &model.Channel{Name: "cnt-enabled", Type: constant.ChannelTypeOpenAI, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "m1,m0"}
	disabledCh := &model.Channel{Name: "cnt-disabled", Type: constant.ChannelTypeOpenAI, Key: "sk", Status: common.ChannelStatusManuallyDisabled, Group: "default", Models: "m2"}
	require.NoError(t, db.Create(enabledCh).Error)
	require.NoError(t, db.Create(disabledCh).Error)

	lane := &model.Lane{
		Name:    "lane-counts",
		Enabled: true,
		Mode:    model.LaneModeFailover,
		Members: []model.LaneMember{
			{ChannelId: enabledCh.Id, Model: "m1", Priority: 100},
			{ChannelId: enabledCh.Id, Model: "m0", Priority: 90, Disabled: true},
			{ChannelId: disabledCh.Id, Model: "m2", Priority: 80},
		},
	}
	require.NoError(t, model.UpsertLane(lane))
	stored, err := model.GetLaneByName("lane-counts")
	require.NoError(t, err)
	// 悬空成员：渠道不存在（绕过 UpsertLane 的渠道校验，模拟渠道被删后的残留）。
	require.NoError(t, db.Create(&model.LaneMember{
		LaneId: stored.Id, ChannelId: 999999, Model: "m3", Priority: 70,
	}).Error)

	summaries, err := model.ListModelSummaries()
	require.NoError(t, err)
	var target *model.ModelSummary
	for i := range summaries {
		if summaries[i].Model == "lane-counts" {
			target = &summaries[i]
			break
		}
	}
	require.NotNil(t, target)
	assert.Equal(t, 4, target.MemberCount, "成员数按展开后口径")
	assert.Equal(t, 1, target.AvailableMemberCount)
	assert.Equal(t, 1, target.DisabledMemberCount, "人工停用")
	assert.Equal(t, 1, target.ChannelDisabledMemberCount, "渠道停用")
	assert.Equal(t, 1, target.OrphanMemberCount, "渠道不存在")
	assert.Equal(t,
		target.MemberCount,
		target.AvailableMemberCount+target.DisabledMemberCount+target.ChannelDisabledMemberCount+target.OrphanMemberCount,
		"四类必须互斥覆盖全部成员")
}

// reset 端点：`reset` 是 circuits 兼容字段，`cleared` 是结构化明细。
func TestResetLaneCircuitsStructuredResponse(t *testing.T) {
	db := setupAPITestDB(t)

	ch := &model.Channel{Name: "reset-ch", Type: constant.ChannelTypeOpenAI, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "m"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, model.UpsertLane(&model.Lane{
		Name: "lane-reset-shape", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{ChannelId: ch.Id, Model: "m", Priority: 100}},
	}))
	t.Cleanup(func() { route.Default.For("lane-reset-shape").Reset() })

	resolved, err := model.ResolveRoute("lane-reset-shape")
	require.NoError(t, err)
	// 预算 1 才能在第一次失败后立即落冷却（默认预算会先原地重试）。
	resolved.Config.MemberMaxAttempts = 1
	state := route.NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	_, _, _ = state.Next(types.NewOpenAIError(errors.New("boom"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError))

	recorder := callAPI(t, http.MethodPost, "/api/v1/lanes/lane-reset-shape/circuits/reset", "",
		ResetLaneCircuits, gin.Params{{Key: "name", Value: "lane-reset-shape"}})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var response struct {
		Lane    string `json:"lane"`
		Reset   int    `json:"reset"`
		Cleared struct {
			Circuits  int  `json:"circuits"`
			Cooldowns int  `json:"cooldowns"`
			Paces     int  `json:"paces"`
			Probe     int  `json:"probe"`
			Affinity  int  `json:"affinity"`
			LaneDown  bool `json:"lane_down"`
		} `json:"cleared"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "lane-reset-shape", response.Lane)
	assert.Equal(t, response.Cleared.Circuits, response.Reset, "reset 必须是 circuits 的数字兼容字段")
	assert.GreaterOrEqual(t, response.Cleared.Cooldowns, 1)
	assert.GreaterOrEqual(t, response.Cleared.Circuits, 1)
}

func TestListLogsErrorKindFilterAndStrictBool(t *testing.T) {
	db := setupAPITestDB(t)
	require.NoError(t, db.Exec("DELETE FROM pbr_request_logs").Error)

	entries := []model.PBRRequestLog{
		{Ts: 1_700_000_000, RequestModel: "m1", Success: true, ErrorKind: string(route.KindLocalThrottle)},
		{Ts: 1_700_000_001, RequestModel: "m2", Success: false, ErrorKind: string(route.KindTimeout)},
	}
	for i := range entries {
		require.NoError(t, db.Create(&entries[i]).Error)
	}

	recorder := callAPI(t, http.MethodGet, "/api/v1/logs?error_kind=local_throttle&page=1", "", ListLogs, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response struct {
		Items []struct {
			ErrorKind string `json:"error_kind"`
			Success   bool   `json:"success"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Items, 1)
	assert.Equal(t, string(route.KindLocalThrottle), response.Items[0].ErrorKind)
	assert.True(t, response.Items[0].Success, "local_throttle 可以是 success=true 的日志（提交后/本地限流口径）")

	recorder = callAPI(t, http.MethodGet, "/api/v1/logs?success=maybe", "", ListLogs, nil)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "validation_failed")
}
