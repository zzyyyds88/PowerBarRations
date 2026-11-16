package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupGroupReferencedLane(t *testing.T) (*model.Channel, *model.MemberGroup, *model.Lane) {
	t.Helper()
	db := model.DB
	channel := &model.Channel{
		Name: "group-ref-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "m-1,m-2",
	}
	require.NoError(t, db.Create(channel).Error)
	group := &model.MemberGroup{Name: "group-ref", Members: []model.MemberGroupItem{
		{ChannelId: channel.Id, Model: "m-1", Priority: 1},
	}}
	require.NoError(t, model.UpsertMemberGroup(group))
	lane := &model.Lane{
		Name: "group-ref-lane", Enabled: true, Mode: model.LaneModeFailover,
		Members: []model.LaneMember{{GroupId: group.Id, Priority: 1}},
	}
	require.NoError(t, model.UpsertLane(lane))
	return channel, group, lane
}

func TestDeleteChannelBlockedByMemberGroup(t *testing.T) {
	setupAPITestDB(t)
	channel, group, _ := setupGroupReferencedLane(t)

	recorder := callAPI(t, http.MethodDelete, "/api/v1/channels/"+channel.Name, "", DeleteChannel,
		gin.Params{{Key: "name", Value: channel.Name}})
	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Lanes  []string `json:"lanes"`
				Groups []string `json:"groups"`
			} `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, "conflict", body.Error.Code)
	assert.Contains(t, body.Error.Details.Lanes, "group-ref-lane")
	assert.Contains(t, body.Error.Details.Groups, group.Name)

	// 渠道必须还在。
	_, err := model.GetChannelByName(channel.Name)
	require.NoError(t, err)
}

func TestPutChannelModelRemovalBlockedByMemberGroup(t *testing.T) {
	setupAPITestDB(t)
	channel, group, _ := setupGroupReferencedLane(t)

	// 即使带 ?force=1 也不能静默改组，必须要求用户编辑成员组。
	recorder := callAPI(t, http.MethodPut,
		"/api/v1/channels/"+channel.Name+"?force=1",
		`{"models":["m-2"]}`, PutChannel,
		gin.Params{{Key: "name", Value: channel.Name}})
	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())

	var body struct {
		Error struct {
			Details struct {
				Groups []string `json:"groups"`
			} `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Contains(t, body.Error.Details.Groups, group.Name)

	saved, err := model.GetChannelByName(channel.Name)
	require.NoError(t, err)
	assert.Equal(t, []string{"m-1", "m-2"}, saved.GetModels(), "被拒绝时不得落库")
}

func TestCleanupLaneMembersDeletesOrphanGroup(t *testing.T) {
	setupAPITestDB(t)
	channel, group, lane := setupGroupReferencedLane(t)
	// 绕过渠道删除守卫，制造历史悬空组项。
	require.NoError(t, model.DB.Delete(&model.Channel{}, channel.Id).Error)

	recorder := callAPI(t, http.MethodPost, "/api/v1/lanes/cleanup-members", "", CleanupLaneMembers, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var body struct {
		CleanedLanes  []string `json:"cleaned_lanes"`
		DeletedLanes  []string `json:"deleted_lanes"`
		CleanedGroups []string `json:"cleaned_groups"`
		DeletedGroups []string `json:"deleted_groups"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Contains(t, body.DeletedGroups, group.Name)
	assert.Contains(t, body.DeletedLanes, lane.Name)

	_, err := model.GetLaneByName(lane.Name)
	assert.Error(t, err)
}
