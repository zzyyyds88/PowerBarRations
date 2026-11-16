package model

import (
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newReferenceTestChannel(t *testing.T, name, models string) *Channel {
	t.Helper()
	channel := &Channel{
		Name:   name,
		Type:   1,
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: models,
	}
	require.NoError(t, DB.Create(channel).Error)
	return channel
}

func TestChannelReferencesIncludesMemberGroup(t *testing.T) {
	truncateTables(t)
	channel := newReferenceTestChannel(t, "ref-ch", "m-1")
	group := &MemberGroup{Name: "ref-group", Members: []MemberGroupItem{
		{ChannelId: channel.Id, Model: "m-1", Priority: 1},
	}}
	require.NoError(t, UpsertMemberGroup(group))
	require.NoError(t, UpsertLane(&Lane{
		Name: "ref-lane", Enabled: true, Mode: LaneModeFailover,
		Members: []LaneMember{{GroupId: group.Id, Priority: 1}},
	}))

	refs, err := ChannelReferences(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"ref-lane"}, refs.Lanes)
	assert.Equal(t, []string{"ref-group"}, refs.Groups)
}

func TestRemovedModelReferencesIncludesMemberGroup(t *testing.T) {
	truncateTables(t)
	channel := newReferenceTestChannel(t, "removed-ch", "m-1,m-2")
	group := &MemberGroup{Name: "removed-group", Members: []MemberGroupItem{
		{ChannelId: channel.Id, Model: "m-1", Priority: 1},
	}}
	require.NoError(t, UpsertMemberGroup(group))
	require.NoError(t, UpsertLane(&Lane{
		Name: "removed-lane", Enabled: true, Mode: LaneModeFailover,
		Members: []LaneMember{{GroupId: group.Id, Priority: 1}},
	}))

	refs, err := RemovedModelReferences(channel.Id, []string{"m-1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"removed-lane"}, refs.Lanes)
	assert.Equal(t, []string{"removed-group"}, refs.Groups)

	unused, err := RemovedModelReferences(channel.Id, []string{"m-2"})
	require.NoError(t, err)
	assert.Empty(t, untouchedOrNil(unused.Lanes))
	assert.Empty(t, untouchedOrNil(unused.Groups))
}

func TestCleanupOrphanLaneMembersKeepsGroupRefs(t *testing.T) {
	truncateTables(t)
	channel := newReferenceTestChannel(t, "orphan-keep-ch", "m-1")
	group := &MemberGroup{Name: "orphan-keep-group", Members: []MemberGroupItem{
		{ChannelId: channel.Id, Model: "m-1", Priority: 1},
	}}
	require.NoError(t, UpsertMemberGroup(group))
	require.NoError(t, UpsertLane(&Lane{
		Name: "orphan-keep-lane", Enabled: true, Mode: LaneModeFailover,
		Members: []LaneMember{
			{GroupId: group.Id, Priority: 2},
			{ChannelId: 999999, Model: "gone", Priority: 1},
		},
	}))

	cleaned, deleted, err := CleanupOrphanLaneMembers()
	require.NoError(t, err)
	assert.Contains(t, cleaned, "orphan-keep-lane")
	assert.NotContains(t, deleted, "orphan-keep-lane")

	var members []LaneMember
	require.NoError(t, DB.Where("lane_id = (SELECT id FROM lanes WHERE name = ?)", "orphan-keep-lane").Find(&members).Error)
	require.Len(t, members, 1, "组引用行不能被当成 channel_id=0 的悬空成员删除")
	assert.Equal(t, group.Id, members[0].GroupId)
}

func TestCleanupOrphanMembersDeletesEmptyGroupAndLane(t *testing.T) {
	truncateTables(t)
	channel := newReferenceTestChannel(t, "orphan-group-ch", "m-1")
	group := &MemberGroup{Name: "orphan-group", Members: []MemberGroupItem{
		{ChannelId: channel.Id, Model: "m-1", Priority: 1},
	}}
	require.NoError(t, UpsertMemberGroup(group))
	require.NoError(t, UpsertLane(&Lane{
		Name: "orphan-group-lane", Enabled: true, Mode: LaneModeFailover,
		Members: []LaneMember{{GroupId: group.Id, Priority: 1}},
	}))
	// 绕过删除守卫制造历史悬空数据。
	require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)

	result, err := CleanupOrphanMembers()
	require.NoError(t, err)
	assert.Contains(t, result.DeletedGroups, "orphan-group")
	assert.Contains(t, result.DeletedLanes, "orphan-group-lane")

	_, err = GetLaneByName("orphan-group-lane")
	assert.Error(t, err)
	_, err = GetMemberGroupByName("orphan-group")
	assert.ErrorIs(t, err, ErrMemberGroupNotFound)
}

func TestCleanupLanesForRemovedModelsOnlyRemovesMatchingMembers(t *testing.T) {
	truncateTables(t)
	channel := newReferenceTestChannel(t, "clean-match-ch", "m-drop,m-keep")
	require.NoError(t, UpsertLane(&Lane{
		Name: "clean-match-lane", Enabled: true, Mode: LaneModeFailover,
		Members: []LaneMember{
			{ChannelId: channel.Id, Model: "m-keep", Priority: 2},
			{ChannelId: channel.Id, Model: "m-drop", Priority: 1},
		},
	}))

	cleaned, deleted, err := CleanupLanesForRemovedModels(channel.Id, []string{"m-drop"})
	require.NoError(t, err)
	assert.Contains(t, cleaned, "clean-match-lane")
	assert.NotContains(t, deleted, "clean-match-lane")

	var members []LaneMember
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Find(&members).Error)
	require.Len(t, members, 1)
	assert.Equal(t, "m-keep", members[0].Model)
}

func untouchedOrNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
