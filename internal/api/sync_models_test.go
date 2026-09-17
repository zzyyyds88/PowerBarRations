package api

import (
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 审查 B5 + F11 回归：sync-models 的整表覆盖必须保护"会因此失去成员来源的车道"。
//
// 判据（审查 F11 修正）：车道名就是路由键；渠道不再声明该路由键、且该车道里有本渠道
// 成员时才算被引用。此前拿被移除的路由键去比成员的 upstream_model（上游真名），
// 既漏报（成员留空用渠道映射）又误报（上游真名恰好等于被删路由键）。
func TestFindRemovedModelReferencesDetectsLaneUse(t *testing.T) {
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ClientKey{}))

	channel := &model.Channel{Name: "sync-ch", Type: 1, Key: "sk-x", Status: common.ChannelStatusEnabled, Group: "default", Models: "keep-me,drop-me"}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

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

	refs := findRemovedModelReferences(channel.Id, []string{"drop-me"})
	require.Len(t, refs, 1, "车道名命中被移除路由键时必须被发现（漏报回归）")
	assert.Contains(t, refs[0], "drop-me")
	assert.NotContains(t, refs[0], "sync-lane", "上游真名与被删路由键同名不算引用（误报回归）")

	// 车道名未被移除 → 不产生引用。
	assert.Empty(t, findRemovedModelReferences(channel.Id, []string{"keep-me", "never-used"}))
	// 空集合健壮且不 panic。
	assert.Empty(t, findRemovedModelReferences(channel.Id, nil))
	assert.Empty(t, findRemovedModelReferences(channel.Id, []string{"", "  "}))

	// 其它渠道的成员不影响判定。
	otherChannel := &model.Channel{Name: "sync-ch-2", Type: 1, Key: "sk-y", Status: common.ChannelStatusEnabled, Group: "default", Models: "keep-me"}
	require.NoError(t, db.Create(otherChannel).Error)
	assert.Empty(t, findRemovedModelReferences(otherChannel.Id, []string{"drop-me"}))
}
