package api

import (
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 审查 B5 + F11 回归：sync-models / 渠道编辑的整表覆盖必须保护"会因此失去成员来源
// 的车道"。判据：车道名就是路由键；渠道不再声明该路由键、且该车道里有本渠道成员。
func TestRemovedModelLaneRefsDetectsLaneUse(t *testing.T) {
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ClientKey{}))

	channel := &model.Channel{Name: "sync-ch", Type: 1, Key: "sk-x", Status: common.ChannelStatusEnabled, Group: "default", Models: "keep-me,drop-me"}
	require.NoError(t, db.Create(channel).Error)

	// 真正会被打断的情况：车道名 = 被移除的路由键，成员来自本渠道。
	dropped := &model.Lane{Name: "drop-me", Mode: model.LaneModeFailover, Enabled: true, Members: []model.LaneMember{
		{ChannelId: channel.Id, Priority: 1}, // upstream_model 留空 → 用渠道映射/路由键
	}}
	require.NoError(t, model.UpsertLane(dropped))

	// 反例（旧的误报形态）：车道名与上游真名同名，但车道名不在被移除集合里。
	other := &model.Lane{Name: "sync-lane", Mode: model.LaneModeFailover, Enabled: true, Members: []model.LaneMember{
		{ChannelId: channel.Id, UpstreamModel: "drop-me", Priority: 1},
	}}
	require.NoError(t, model.UpsertLane(other))

	refs, err := model.RemovedModelLaneRefs(channel.Id, []string{"drop-me"})
	require.NoError(t, err)
	require.Len(t, refs, 1, "车道名命中被移除路由键时必须被发现（漏报回归）")
	assert.Equal(t, "drop-me", refs[0])
	assert.NotContains(t, refs, "sync-lane", "上游真名与被删路由键同名不算引用（误报回归）")

	// 车道名未被移除 → 不产生引用。
	unused, err := model.RemovedModelLaneRefs(channel.Id, []string{"keep-me", "never-used"})
	require.NoError(t, err)
	assert.Empty(t, unused)
	// 空集合健壮且不 panic。
	empty, err := model.RemovedModelLaneRefs(channel.Id, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
	blank, err := model.RemovedModelLaneRefs(channel.Id, []string{"", "  "})
	require.NoError(t, err)
	assert.Empty(t, blank)

	// 其它渠道的成员不影响判定。
	otherChannel := &model.Channel{Name: "sync-ch-2", Type: 1, Key: "sk-y", Status: common.ChannelStatusEnabled, Group: "default", Models: "keep-me"}
	require.NoError(t, db.Create(otherChannel).Error)
	foreign, err := model.RemovedModelLaneRefs(otherChannel.Id, []string{"drop-me"})
	require.NoError(t, err)
	assert.Empty(t, foreign)
}

// CleanupLanesForRemovedModels：只移除本渠道成员；空车道删除；仍有他渠道成员的车道保留。
func TestCleanupLanesForRemovedModels(t *testing.T) {
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ClientKey{}))

	chA := &model.Channel{Name: "clean-a", Type: 1, Key: "sk-a", Status: common.ChannelStatusEnabled, Group: "default", Models: "m-drop,m-keep"}
	require.NoError(t, db.Create(chA).Error)
	chB := &model.Channel{Name: "clean-b", Type: 1, Key: "sk-b", Status: common.ChannelStatusEnabled, Group: "default", Models: "m-drop"}
	require.NoError(t, db.Create(chB).Error)

	require.NoError(t, model.UpsertLane(&model.Lane{Name: "m-drop", Enabled: true, Mode: model.LaneModeFailover, Members: []model.LaneMember{
		{ChannelId: chA.Id, Priority: 2}, {ChannelId: chB.Id, Priority: 1},
	}}))
	require.NoError(t, model.UpsertLane(&model.Lane{Name: "m-keep", Enabled: true, Mode: model.LaneModeFailover, Members: []model.LaneMember{
		{ChannelId: chA.Id, Priority: 1},
	}}))

	cleaned, deleted, err := model.CleanupLanesForRemovedModels(chA.Id, []string{"m-drop", "m-keep"})
	require.NoError(t, err)
	assert.Contains(t, cleaned, "m-drop", "仍有他渠道成员 → 只清理不删除")
	assert.Contains(t, deleted, "m-keep", "成员被清空 → 删除车道")

	lane, err := model.GetLaneByName("m-drop")
	require.NoError(t, err)
	var members []model.LaneMember
	require.NoError(t, db.Where("lane_id = ?", lane.Id).Find(&members).Error)
	require.Len(t, members, 1, "只应移除本渠道成员")
	assert.Equal(t, chB.Id, members[0].ChannelId)

	_, err = model.GetLaneByName("m-keep")
	assert.Error(t, err, "空车道应已被删除")
}
