package api

import (
	"testing"

	"pbr/common"
	"pbr/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 审查 B5 回归：sync-models 的整表覆盖必须保护仍在被引用的模型。
func TestFindRemovedModelReferencesDetectsLaneUse(t *testing.T) {
	db := setupAPITestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ClientKey{}))

	channel := &model.Channel{Name: "sync-ch", Type: 1, Key: "sk-x", Status: common.ChannelStatusEnabled, Group: "default", Models: "keep-me,drop-me"}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

	// 建一条显式车道，成员点名 drop-me 作为 upstream_model。
	lane := &model.Lane{Name: "sync-lane", Mode: model.LaneModeFailover, Enabled: true, Members: []model.LaneMember{
		{ChannelId: channel.Id, UpstreamModel: "drop-me", Priority: 1},
	}}
	require.NoError(t, model.UpsertLane(lane))

	refs := findRemovedModelReferences(channel.Id, []string{"drop-me"})
	require.Len(t, refs, 1, "被车道引用的模型必须被发现")
	assert.Contains(t, refs[0], "sync-lane")

	// 未被引用的模型不产生引用。
	assert.Empty(t, findRemovedModelReferences(channel.Id, []string{"keep-me", "never-used"}))
	// 空集合健壮且不 panic。
	assert.Empty(t, findRemovedModelReferences(channel.Id, nil))
	assert.Empty(t, findRemovedModelReferences(channel.Id, []string{"", "  "}))
}
