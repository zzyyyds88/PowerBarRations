package api

import (
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 导入时悬空成员不再让整包 422：跳过该成员并记录 warning / diff.skipped；
// 成员被清空的启用车道改为停用（而不是写入"启用但无成员"的非法车道）。
func TestPruneMissingMemberChannelsSkipsOrphans(t *testing.T) {
	db := setupAPITestDB(t)
	ch := &model.Channel{Name: "real-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "m"}
	require.NoError(t, db.Create(ch).Error)

	bundle := &ConfigBundle{
		Version: "v1",
		Lanes: []LaneConfig{
			{
				Name: "mixed", Enabled: true, Mode: model.LaneModeFailover,
				Members: []LaneMemberConfig{
					{Channel: "real-ch", UpstreamModel: "m", Priority: 2},
					{Channel: "ghost-ch", UpstreamModel: "m", Priority: 1},
				},
			},
			{
				Name: "all-ghost", Enabled: true, Mode: model.LaneModeFailover,
				Members: []LaneMemberConfig{{Channel: "ghost-ch", UpstreamModel: "m", Priority: 1}},
			},
		},
	}
	result := ImportResult{Valid: true, Diff: map[string]any{"lanes": newDiffList()}}

	pruneMissingMemberChannels(bundle, &result)

	// mixed：只保留真实渠道成员
	require.Len(t, bundle.Lanes[0].Members, 1)
	assert.Equal(t, "real-ch", bundle.Lanes[0].Members[0].Channel)
	assert.True(t, bundle.Lanes[0].Enabled)
	// all-ghost：成员清空 → 停用
	assert.Empty(t, bundle.Lanes[1].Members)
	assert.False(t, bundle.Lanes[1].Enabled)
	// 提示可见：两条"跳过成员" + 一条"整条车道无有效成员、改为停用"
	require.Len(t, result.Warnings, 3)
	assert.Contains(t, result.Warnings[0], "ghost-ch")
	assert.Contains(t, result.Warnings[1], "ghost-ch")
	assert.Contains(t, result.Warnings[2], "all-ghost")
	assert.Contains(t, result.Warnings[2], "disabled")
	diff, ok := result.Diff["lanes"].(DiffList)
	require.True(t, ok)
	assert.NotEmpty(t, diff.Skipped)
}
