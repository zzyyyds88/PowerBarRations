package model

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/zzyyyds88/PowerBarRations/common"
)

// 渠道退役编排（design-v1 §7.9）：停用 → 解除引用 → 删除 → 历史标记。
// 这条链是"删除入口"的核心承诺：确认一次，系统替运维摘干净，不留悬空引用，
// 也不把同名新渠道的历史混在一起。

func setupRetireDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	original := DB
	DB = db
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { DB = original })
	require.NoError(t, db.AutoMigrate(
		&Channel{}, &Lane{}, &LaneMember{}, &MemberGroup{}, &MemberGroupItem{},
		&PBRRequestLog{}, &PBRStatsHourly{}, &PBRAsyncObject{},
	))
}

func retireSeedChannel(t *testing.T, name string, id int) *Channel {
	t.Helper()
	ch := &Channel{Id: id, Name: name, Models: name + "-model", Status: 1}
	require.NoError(t, DB.Create(ch).Error)
	return ch
}

func retireSeedLane(t *testing.T, name string) *Lane {
	t.Helper()
	lane := &Lane{Name: name, Enabled: true, Mode: "failover"}
	require.NoError(t, DB.Create(lane).Error)
	return lane
}

func retireSeedLog(t *testing.T, channelID int, channelName, lane string, cost float64) {
	t.Helper()
	require.NoError(t, DB.Create(&PBRRequestLog{
		Ts:                time.Now().Unix(),
		LaneName:          lane,
		MemberChannelId:   channelID,
		MemberChannelName: channelName,
		EstimatedCost:     cost,
	}).Error)
}

// 正向：散成员被摘、空车道整条删除、历史按 id 改写、渠道行消失。
func TestRetireChannelRemovesReferencesAndMarksHistory(t *testing.T) {
	setupRetireDB(t)
	ch := retireSeedChannel(t, "old-upstream", 7)
	laneKeep := retireSeedLane(t, "keep-lane")
	laneDrop := retireSeedLane(t, "drop-lane")
	require.NoError(t, DB.Create(&LaneMember{LaneId: laneKeep.Id, ChannelId: 8, Model: "other-model"}).Error)
	require.NoError(t, DB.Create(&LaneMember{LaneId: laneKeep.Id, ChannelId: ch.Id, Model: ch.Models}).Error)
	require.NoError(t, DB.Create(&LaneMember{LaneId: laneDrop.Id, ChannelId: ch.Id, Model: ch.Models}).Error)
	retireSeedLog(t, ch.Id, "old-upstream", "keep-lane", 0.5)
	retireSeedLog(t, 99, "unrelated", "keep-lane", 0.1)

	result, err := RetireChannel("old-upstream", time.Date(2026, 11, 21, 15, 4, 0, 0, time.Local))
	require.NoError(t, err)

	assert.True(t, result.Retired)
	assert.Equal(t, "old-upstream（已退役 2026-11-21 15:04）", result.RetiredAs)
	assert.True(t, result.Disabled, "退役先置停用")
	assert.Equal(t, []string{"keep-lane"}, result.LanesCleaned)
	assert.Equal(t, []string{"drop-lane"}, result.LanesDeleted)
	assert.Empty(t, result.GroupsAffected)

	// 渠道行删除；散成员无残留；空车道整条删除；保留车道仍在。
	var count int64
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", ch.Id).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.Model(&LaneMember{}).Where("channel_id = ?", ch.Id).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.Model(&Lane{}).Where("name = ?", "drop-lane").Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.Model(&Lane{}).Where("name = ?", "keep-lane").Count(&count).Error)
	assert.EqualValues(t, 1, count)

	// 历史按 member_channel_id 精确改写：该渠道的行改名，无关渠道原样保留。
	var renamed []PBRRequestLog
	require.NoError(t, DB.Where("member_channel_id = ?", ch.Id).Find(&renamed).Error)
	require.Len(t, renamed, 1)
	assert.Equal(t, "old-upstream（已退役 2026-11-21 15:04）", renamed[0].MemberChannelName)
	var untouched []PBRRequestLog
	require.NoError(t, DB.Where("member_channel_id = ?", 99).Find(&untouched).Error)
	require.Len(t, untouched, 1)
	assert.Equal(t, "unrelated", untouched[0].MemberChannelName)
}

// 成员组：条目被摘、组本身保留、变空组单独列出；通过组引用本渠道且
// 摘除后零有效成员的车道一并删除。
func TestRetireChannelRemovesGroupItemsButKeepsGroups(t *testing.T) {
	setupRetireDB(t)
	ch := retireSeedChannel(t, "grouped-ch", 11)
	group := &MemberGroup{Name: "shared-pool"}
	require.NoError(t, DB.Create(group).Error)
	require.NoError(t, DB.Create(&MemberGroupItem{GroupId: group.Id, ChannelId: ch.Id, Model: ch.Models}).Error)
	laneDrop := retireSeedLane(t, "via-group-drop")
	require.NoError(t, DB.Create(&LaneMember{LaneId: laneDrop.Id, GroupId: group.Id, Priority: 5}).Error)
	laneKeep := retireSeedLane(t, "via-group-keep")
	require.NoError(t, DB.Create(&LaneMember{LaneId: laneKeep.Id, GroupId: group.Id, Priority: 5}).Error)
	require.NoError(t, DB.Create(&LaneMember{LaneId: laneKeep.Id, ChannelId: 8, Model: "fallback"}).Error)
	retireSeedLog(t, ch.Id, "grouped-ch", "via-group-keep", 0.2)

	result, err := RetireChannel("grouped-ch", time.Date(2026, 11, 21, 15, 4, 0, 0, time.Local))
	require.NoError(t, err)

	assert.Equal(t, []string{"shared-pool"}, result.GroupsAffected)
	assert.Equal(t, []string{"shared-pool"}, result.GroupsEmptied, "组内只剩本渠道 → 摘除后变空")
	assert.Equal(t, []string{"via-group-drop"}, result.LanesDeleted)
	assert.Equal(t, []string{"via-group-keep"}, result.LanesCleaned)

	// 组本身保留（复用模板，去留由运维决定），组条目清空。
	var groups int64
	require.NoError(t, DB.Model(&MemberGroup{}).Where("id = ?", group.Id).Count(&groups).Error)
	assert.EqualValues(t, 1, groups)
	var items int64
	require.NoError(t, DB.Model(&MemberGroupItem{}).Where("group_id = ?", group.Id).Count(&items).Error)
	assert.Zero(t, items)
	var laneCount int64
	require.NoError(t, DB.Model(&Lane{}).Where("name IN ?", []string{"via-group-drop", "via-group-keep"}).Count(&laneCount).Error)
	assert.EqualValues(t, 1, laneCount, "只剩 via-group-keep")
}

// 统计改写：channel 维度整键改名，channel_model 维度只改渠道头、保留模型段；
// 其他渠道与模型维度的行不受影响。
func TestRetireChannelRewritesStatsGroupKeys(t *testing.T) {
	setupRetireDB(t)
	ch := retireSeedChannel(t, "stat-ch", 21)
	lane := retireSeedLane(t, "stat-lane")
	require.NoError(t, DB.Create(&LaneMember{LaneId: lane.Id, ChannelId: ch.Id, Model: ch.Models}).Error)
	bucket := (time.Now().Unix() / 3600) * 3600
	rows := []PBRStatsHourly{
		{BucketTs: bucket, GroupKind: "channel", GroupKey: "stat-ch"},
		{BucketTs: bucket, GroupKind: "channel_model", GroupKey: "stat-ch" + PBRChannelModelSeparator + "m1"},
		{BucketTs: bucket, GroupKind: "channel", GroupKey: "other-ch"},
		{BucketTs: bucket, GroupKind: "channel_model", GroupKey: "other-ch" + PBRChannelModelSeparator + "m2"},
		{BucketTs: bucket, GroupKind: "lane", GroupKey: "stat-lane"},
	}
	for i := range rows {
		require.NoError(t, DB.Create(&rows[i]).Error)
	}

	result, err := RetireChannel("stat-ch", time.Date(2026, 11, 21, 15, 4, 0, 0, time.Local))
	require.NoError(t, err)
	assert.EqualValues(t, 2, result.StatsRowsRenamed)

	var keys []string
	require.NoError(t, DB.Model(&PBRStatsHourly{}).Where("group_kind IN ?", []string{"channel", "channel_model"}).
		Order("group_key").Pluck("group_key", &keys).Error)
	assert.Equal(t, []string{
		"other-ch",
		"other-ch" + PBRChannelModelSeparator + "m2",
		"stat-ch（已退役 2026-11-21 15:04）",
		"stat-ch（已退役 2026-11-21 15:04）" + PBRChannelModelSeparator + "m1",
	}, keys)
}

// 同名新渠道不受退役历史影响：旧 id 的历史已带标记，新 id 的新写入保持原名。
func TestRetireChannelHistoryIsScopedByChannelID(t *testing.T) {
	setupRetireDB(t)
	ch := retireSeedChannel(t, "reuse", 31)
	lane := retireSeedLane(t, "reuse-lane")
	require.NoError(t, DB.Create(&LaneMember{LaneId: lane.Id, ChannelId: ch.Id, Model: ch.Models}).Error)
	retireSeedLog(t, ch.Id, "reuse", "reuse-lane", 0.3)

	_, err := RetireChannel("reuse", time.Date(2026, 11, 21, 15, 4, 0, 0, time.Local))
	require.NoError(t, err)

	// 同名重建（新 id）。
	require.NoError(t, DB.Create(&Channel{Id: 32, Name: "reuse", Models: "reuse-model", Status: 1}).Error)
	retireSeedLog(t, 32, "reuse", "reuse-lane", 0.4)

	var names []string
	require.NoError(t, DB.Model(&PBRRequestLog{}).Order("id").Pluck("member_channel_name", &names).Error)
	assert.Equal(t, []string{"reuse（已退役 2026-11-21 15:04）", "reuse"}, names)
}

// 预览与执行同判定：同一份清单、不落库。
func TestRetirePlanMatchesExecution(t *testing.T) {
	setupRetireDB(t)
	ch := retireSeedChannel(t, "plan-ch", 41)
	lane := retireSeedLane(t, "plan-lane")
	require.NoError(t, DB.Create(&LaneMember{LaneId: lane.Id, ChannelId: ch.Id, Model: ch.Models}).Error)
	retireSeedLog(t, ch.Id, "plan-ch", "plan-lane", 0.6)

	plan, err := GetRetirePlan("plan-ch")
	require.NoError(t, err)
	assert.True(t, plan.Disable)
	assert.Equal(t, []string{"plan-lane"}, plan.LanesDeleted)
	assert.Empty(t, plan.LanesCleaned)
	assert.EqualValues(t, 1, plan.LogsRename)

	// 预览后渠道与数据原样。
	var count int64
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", ch.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, DB.Model(&LaneMember{}).Where("channel_id = ?", ch.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

// 已停用的渠道退役时不再重复置停用；不存在渠道按既有 not-found 语义。
func TestRetireChannelDisabledAndMissing(t *testing.T) {
	setupRetireDB(t)
	ch := retireSeedChannel(t, "already-off", 51)
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", ch.Id).
		Update("status", common.ChannelStatusManuallyDisabled).Error)

	result, err := RetireChannel("already-off", time.Now())
	require.NoError(t, err)
	assert.False(t, result.Disabled, "已停用无需再置")

	_, err = RetireChannel("no-such-channel", time.Now())
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
