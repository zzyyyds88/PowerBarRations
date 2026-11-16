package api

import (
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 审查 B5 + F11 回归：sync-models / 渠道编辑的整表覆盖必须保护"会因此失去成员来源
// 判据：成员的**所选模型**是否命中被移除集合（ADR 0006/0008）；历史空 model
// 按车道名兜底。车道名与成员模型可以不同，不能只按车道名匹配。
func TestRemovedModelLaneRefsDetectsLaneUse(t *testing.T) {
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ClientKey{}))

	channel := &model.Channel{Name: "sync-ch", Type: 1, Key: "sk-x", Status: common.ChannelStatusEnabled, Group: "default", Models: "keep-me,drop-me"}
	require.NoError(t, db.Create(channel).Error)

	// 历史形态：成员 model 为空，按车道名兜底命中。
	dropped := &model.Lane{Name: "drop-me", Mode: model.LaneModeFailover, Enabled: true, Members: []model.LaneMember{
		{ChannelId: channel.Id, Priority: 1},
	}}
	require.NoError(t, model.UpsertLane(dropped))

	// 现行形态：车道名与成员模型不同，但成员所选模型命中，必须算引用。
	other := &model.Lane{Name: "sync-lane", Mode: model.LaneModeFailover, Enabled: true, Members: []model.LaneMember{
		{ChannelId: channel.Id, Model: "drop-me", Priority: 1},
	}}
	require.NoError(t, model.UpsertLane(other))

	refs, err := model.RemovedModelLaneRefs(channel.Id, []string{"drop-me"})
	require.NoError(t, err)
	require.Len(t, refs, 2, "成员所选模型命中必须被发现（漏报回归）")
	assert.Contains(t, refs, "drop-me")
	assert.Contains(t, refs, "sync-lane")

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
