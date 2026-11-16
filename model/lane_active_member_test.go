package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newActiveMemberTestLane(t *testing.T, name string, channelIDs []int, active string) *Lane {
	t.Helper()
	lane := &Lane{
		Name:         name,
		Enabled:      true,
		Mode:         LaneModeManual,
		ActiveMember: active,
	}
	for i, channelID := range channelIDs {
		lane.Members = append(lane.Members, LaneMember{
			ChannelId: channelID,
			Model:     "m-1",
			Priority:  len(channelIDs) - i,
		})
	}
	require.NoError(t, UpsertLane(lane))
	return lane
}

func TestManualActiveMemberValidationAndReconciliation(t *testing.T) {
	truncateTables(t)
	channelA := newReferenceTestChannel(t, "active-a", "m-1")
	channelB := newReferenceTestChannel(t, "active-b", "m-1")
	lane := newActiveMemberTestLane(t, "active-lane", []int{channelA.Id, channelB.Id}, "active-a/m-1")
	require.NoError(t, ValidateLaneActiveMember(lane))

	cleaned, deleted, err := CleanupLanesForRemovedModelsTx(DB, channelA.Id, []string{"m-1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"active-lane"}, cleaned)
	assert.Empty(t, deleted)

	// 清理后点名标签悬空；reconcile 只清空 pin，不自动换人。
	cleared, err := ReconcileManualActiveMembersByNames([]string{"active-lane"})
	require.NoError(t, err)
	assert.Equal(t, []string{"active-lane"}, cleared)
	saved, err := GetLaneByName("active-lane")
	require.NoError(t, err)
	assert.Empty(t, saved.ActiveMember)

	// 管理面必须报告 0 个可用成员，而不是“还有 channel-b 可用”。
	summaries, err := ListModelSummaries()
	require.NoError(t, err)
	for _, summary := range summaries {
		if summary.Model == "active-lane" {
			assert.Equal(t, 1, summary.MemberCount)
			assert.Equal(t, 0, summary.AvailableMemberCount)
			return
		}
	}
	t.Fatal("active-lane not found in summaries")
}

func TestManualActiveMemberValidationRejectsMissingPin(t *testing.T) {
	truncateTables(t)
	channel := newReferenceTestChannel(t, "invalid-active", "m-1")
	lane := &Lane{
		Name:         "invalid-lane",
		Enabled:      true,
		Mode:         LaneModeManual,
		ActiveMember: "missing-channel/m-1",
		Members:      []LaneMember{{ChannelId: channel.Id, Model: "m-1", Priority: 1}},
	}
	err := ValidateLaneActiveMember(lane)
	require.Error(t, err)
	var activeErr *LaneActiveMemberError
	require.ErrorAs(t, err, &activeErr)
	assert.Equal(t, "not_found", activeErr.Reason)
}

func TestRewriteManualActiveMemberChannelName(t *testing.T) {
	truncateTables(t)
	channel := newReferenceTestChannel(t, "old-name", "m-1")
	newActiveMemberTestLane(t, "rename-lane", []int{channel.Id}, "old-name/m-1")

	changed, err := RewriteManualActiveMemberChannelName(DB, "old-name", "new-name")
	require.NoError(t, err)
	assert.Equal(t, []string{"rename-lane"}, changed)
	saved, err := GetLaneByName("rename-lane")
	require.NoError(t, err)
	assert.Equal(t, "new-name/m-1", saved.ActiveMember)
}

func TestActiveMemberIndexMatchesAliasThenLabel(t *testing.T) {
	members := []RouteMember{
		{Channel: "ch", Model: "m", PublicAlias: "primary"},
		{Channel: "ch", Model: "m2"},
	}
	assert.Equal(t, 0, ActiveMemberIndex("primary", members))
	assert.Equal(t, 1, ActiveMemberIndex("ch/m2", members))
	assert.Equal(t, -1, ActiveMemberIndex("ch/missing", members))
}
