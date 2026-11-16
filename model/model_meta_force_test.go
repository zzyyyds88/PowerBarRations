package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteModelMetadataForceCleansDirectLaneMembers(t *testing.T) {
	truncateTables(t)
	channel := newReferenceTestChannel(t, "meta-force-ch", "m-1")
	require.NoError(t, UpsertLane(&Lane{
		Name:    "m-1",
		Enabled: true,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{ChannelId: channel.Id, Model: "m-1", Priority: 1}},
	}))
	record := &Model{ModelName: "m-1", Status: 1, NameRule: NameRuleExact}
	require.NoError(t, DB.Create(record).Error)

	result, err := DeleteModelMetadataWithOptions([]int{record.Id}, true, false, true)
	require.NoError(t, err)
	assert.Equal(t, 1, result.UpdatedChannels)
	assert.Equal(t, []string{"m-1"}, result.DeletedLanes)
	assert.Empty(t, result.CleanedLanes)

	saved, err := GetChannelById(channel.Id, false)
	require.NoError(t, err)
	assert.Empty(t, saved.GetModels())

	_, err = GetLaneByName("m-1")
	require.Error(t, err)
}

func TestDeleteModelMetadataForceStillBlocksMemberGroups(t *testing.T) {
	truncateTables(t)
	channel := newReferenceTestChannel(t, "meta-force-group-ch", "m-1")
	group := &MemberGroup{Name: "meta-force-group", Members: []MemberGroupItem{
		{ChannelId: channel.Id, Model: "m-1", Priority: 1},
	}}
	require.NoError(t, UpsertMemberGroup(group))
	record := &Model{ModelName: "m-1", Status: 1, NameRule: NameRuleExact}
	require.NoError(t, DB.Create(record).Error)

	_, err := DeleteModelMetadataWithOptions([]int{record.Id}, true, false, true)
	require.Error(t, err)
	var laneErr *LaneReferenceError
	require.ErrorAs(t, err, &laneErr)
	assert.Equal(t, []string{"meta-force-group"}, laneErr.Groups["meta-force-group-ch"])
}
