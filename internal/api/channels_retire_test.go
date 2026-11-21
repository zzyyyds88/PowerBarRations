package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// POST /api/channels/{name}/retire（design-v1 §7.9）：控制台"删除"入口的编排契约。
// dry-run 必须逐项预览且不落库；执行后引用摘净、历史改名、渠道行消失；
// 已删除渠道再退役 → 404。
func TestRetireChannelPreviewThenExecute(t *testing.T) {
	engine, db := setupOpsContractTest(t)

	ch := &model.Channel{Name: "retire-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "retire-model"}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, db.Create(&model.Lane{Name: "retire-lane", Enabled: true, Mode: model.LaneModeFailover}).Error)
	var lane model.Lane
	require.NoError(t, db.Where("name = ?", "retire-lane").First(&lane).Error)
	require.NoError(t, db.Create(&model.LaneMember{LaneId: lane.Id, ChannelId: ch.Id, Model: "retire-model", Priority: 1}).Error)
	require.NoError(t, db.Create(&model.PBRRequestLog{
		Ts:                time.Now().Unix(),
		LaneName:          "retire-lane",
		MemberChannelId:   ch.Id,
		MemberChannelName: "retire-ch",
	}).Error)

	// dry-run：逐项预览，不落库。
	preview := doOpsRequest(engine, http.MethodPost, "/api/v1/channels/retire-ch/retire?dry_run=true", "")
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	body := decodeOpsJSON(t, preview)
	assert.Equal(t, true, body["dry_run"])
	plan, ok := body["plan"].(map[string]any)
	require.True(t, ok, preview.Body.String())
	assert.Equal(t, true, plan["disable"])
	assert.Equal(t, []any{"retire-lane"}, plan["lanes_deleted"])
	assert.EqualValues(t, 1, plan["logs_rename"])

	// 预览不落库：渠道、成员、历史全部原样。
	var count int64
	require.NoError(t, db.Model(&model.Channel{}).Where("name = ?", "retire-ch").Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&model.LaneMember{}).Where("channel_id = ?", ch.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)

	// 执行退役。
	done := doOpsRequest(engine, http.MethodPost, "/api/v1/channels/retire-ch/retire", "")
	require.Equal(t, http.StatusOK, done.Code, done.Body.String())
	result := decodeOpsJSON(t, done)
	assert.Equal(t, true, result["retired"])
	assert.Equal(t, []any{"retire-lane"}, result["lanes_deleted"])
	assert.EqualValues(t, 1, result["logs_renamed"])

	// 车道整条删除；渠道行消失；历史改名带退役标记与时间。
	var laneCount int64
	require.NoError(t, db.Model(&model.Lane{}).Where("name = ?", "retire-lane").Count(&laneCount).Error)
	assert.Zero(t, laneCount)
	var channels int64
	require.NoError(t, db.Model(&model.Channel{}).Where("name = ?", "retire-ch").Count(&channels).Error)
	assert.Zero(t, channels)
	var logs []model.PBRRequestLog
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].MemberChannelName, "已退役")

	// 已删除渠道再退役 → 404 channel_not_found（不是 500/200）。
	gone := doOpsRequest(engine, http.MethodPost, "/api/v1/channels/retire-ch/retire", "")
	require.Equal(t, http.StatusNotFound, gone.Code, gone.Body.String())
}
