package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 成员组核心验收（ADR 0009、routing-spec §1.4）：解析层展开为连续区段、两层开关
// 三处闭合、级联删除、展开后计数。运行态零改动（internal/route 不感知组）由
// internal/route 的既有测试保证——这里的断言全部落在解析结果（ResolvedRoute）上。

func setupGroupTest(t *testing.T) {
	t.Helper()
	setupLaneTest(t)
	require.NoError(t, DB.Exec("DELETE FROM member_group_items").Error)
	require.NoError(t, DB.Exec("DELETE FROM member_groups").Error)
}

func newTestGroup(t *testing.T, name string, items ...MemberGroupItem) *MemberGroup {
	t.Helper()
	group := &MemberGroup{Name: name, Members: items}
	require.NoError(t, UpsertMemberGroup(group))
	saved, err := GetMemberGroupByName(name)
	require.NoError(t, err)
	return saved
}

// 展开：组引用在链上占一个位置，组内成员按组内顺序连续展开（routing-spec §1.4）。
// 链 [M1(p3), G(p2), M2(p1)] → [M1, g1, g2, M2]；组展开成员 MemberId 为负数
// （-member_group_items 主键），与 lane_members 主键空间不冲突。
func TestExpandLaneChainGroupSection(t *testing.T) {
	setupGroupTest(t)
	chA := newTestChannel(t, "ch-a", "m-1")
	chB := newTestChannel(t, "ch-b", "m-2")
	group := newTestGroup(t, "grp",
		MemberGroupItem{ChannelId: chA.Id, Model: "m-1", Priority: 1},
		MemberGroupItem{ChannelId: chB.Id, Model: "m-2", Priority: 2},
	)
	chC := newTestChannel(t, "ch-c", "m-3")
	require.NoError(t, UpsertLane(&Lane{
		Name: "lane-x", Enabled: true, Mode: LaneModeFailover,
		Members: []LaneMember{
			{ChannelId: chC.Id, Model: "m-3", Priority: 3},
			{GroupId: group.Id, Priority: 2},
			{ChannelId: chA.Id, Model: "m-1-alias", Priority: 1},
		},
	}))

	route, err := ResolveRoute("lane-x")
	require.NoError(t, err)
	require.Len(t, route.Members, 4)
	// 链顺序：M1 → [g2(m-2), g1(m-1)] → M2（组内 priority 降序、区段共享车道位置 2）。
	assert.Equal(t, "ch-c", route.Members[0].Channel)
	assert.Equal(t, "ch-b", route.Members[1].Channel, "组内 priority 大者（m-2）先")
	assert.Equal(t, "ch-a", route.Members[2].Channel)
	assert.Equal(t, "ch-a", route.Members[3].Channel)
	assert.Equal(t, "grp", route.Members[1].Group, "组展开成员带来源组名")
	assert.Empty(t, route.Members[0].Group, "散成员无组名")
	for i := 1; i <= 2; i++ {
		assert.Equal(t, 2, route.Members[i].Priority, "区段成员共享组引用的车道位置")
		assert.Less(t, route.Members[i].MemberId, 0, "组展开成员 MemberId 为负数")
	}
	assert.Greater(t, route.Members[0].MemberId, 0, "散成员 MemberId 为 lane_members 主键")
}

// 两层开关三处闭合（routing-spec §1.4）：组全局关、车道引用关、组内成员关，
// 任一命中即该成员 Disabled（选路按既有"人工停用"口径跳过）。
func TestGroupTwoLevelSwitches(t *testing.T) {
	setupGroupTest(t)
	ch := newTestChannel(t, "ch-s", "m-s")
	group := newTestGroup(t, "grp-s",
		MemberGroupItem{ChannelId: ch.Id, Model: "m-s", Priority: 2},
		MemberGroupItem{ChannelId: ch.Id, Model: "m-s2", Priority: 1, Disabled: true},
	)
	upsertLaneWithRef := func() {
		require.NoError(t, UpsertLane(&Lane{
			Name: "lane-s", Enabled: true, Mode: LaneModeFailover,
			Members: []LaneMember{{GroupId: group.Id, Priority: 1}},
		}))
	}
	upsertLaneWithRef()

	route, err := ResolveRoute("lane-s")
	require.NoError(t, err)
	require.Len(t, route.Members, 2)
	assert.False(t, route.Members[0].Disabled, "全开时组内成员参与选路")
	assert.True(t, route.Members[1].Disabled, "组内成员级停用生效")

	// ① 组全局关：引用开关不动，重新解析即全体 Disabled（配置态，不落任何运行态）。
	saved, err := GetMemberGroupByName("grp-s")
	require.NoError(t, err)
	saved.Disabled = true
	require.NoError(t, UpsertMemberGroup(saved))
	route, err = ResolveRoute("lane-s")
	require.NoError(t, err)
	assert.True(t, route.Members[0].Disabled, "组全局关 → 组内成员全部停用")
	assert.True(t, route.Members[1].Disabled)

	// ② 组重新打开 + 车道引用关：仅本车道停用（另一条车道不受影响）。
	saved.Disabled = false
	require.NoError(t, UpsertMemberGroup(saved))
	require.NoError(t, UpsertLane(&Lane{
		Name: "lane-s2", Enabled: true, Mode: LaneModeFailover,
		Members: []LaneMember{{GroupId: group.Id, Priority: 1}},
	}))
	// lane-s 引用关（Disabled=true），lane-s2 引用开。
	require.NoError(t, DB.Model(&LaneMember{}).
		Where("lane_id = ? AND group_id = ?", func() int {
			lane, err := GetLaneByName("lane-s")
			require.NoError(t, err)
			return lane.Id
		}(), group.Id).Update("disabled", true).Error)

	routeS, err := ResolveRoute("lane-s")
	require.NoError(t, err)
	assert.True(t, routeS.Members[0].Disabled, "车道引用关 → 仅本车道停用")
	routeS2, err := ResolveRoute("lane-s2")
	require.NoError(t, err)
	assert.False(t, routeS2.Members[0].Disabled, "另一条车道不受本车道引用开关影响")
}

// 删除组 = 级联清理全部引用；链因此为空的车道整条删除（api-spec §5.2.1）。
func TestDeleteMemberGroupCascade(t *testing.T) {
	setupGroupTest(t)
	ch := newTestChannel(t, "ch-d", "m-d")
	chOther := newTestChannel(t, "ch-other", "m-o")
	group := newTestGroup(t, "grp-d", MemberGroupItem{ChannelId: ch.Id, Model: "m-d", Priority: 1})
	require.NoError(t, UpsertLane(&Lane{
		Name: "lane-only-group", Enabled: true, Mode: LaneModeFailover,
		Members: []LaneMember{{GroupId: group.Id, Priority: 2}, {ChannelId: chOther.Id, Model: "m-o", Priority: 1}},
	}))
	require.NoError(t, UpsertLane(&Lane{
		Name: "lane-all-group", Enabled: true, Mode: LaneModeFailover,
		Members: []LaneMember{{GroupId: group.Id, Priority: 1}},
	}))

	cleaned, deleted, err := DeleteMemberGroupByName("grp-d")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"lane-only-group"}, cleaned, "仍有散成员的车道被清理保留")
	assert.ElementsMatch(t, []string{"lane-all-group"}, deleted, "链空车道整条删除")

	_, err = GetLaneByName("lane-all-group")
	assert.Error(t, err, "被级联删除的车道不再存在")
	lane, err := GetLaneByName("lane-only-group")
	require.NoError(t, err)
	assert.Len(t, lane.Members, 1, "组引用行已从链上移除")
	assert.Equal(t, chOther.Id, lane.Members[0].ChannelId)
}

// 组 CRUD 校验：空成员 422 语义（ErrGroupHasNoMembers）、组内 (渠道, 模型) 唯一、
// 悬空渠道拒绝。
func TestMemberGroupValidation(t *testing.T) {
	setupGroupTest(t)
	ch := newTestChannel(t, "ch-v", "m-v")

	err := UpsertMemberGroup(&MemberGroup{Name: "empty", Members: nil})
	assert.ErrorIs(t, err, ErrGroupHasNoMembers)

	_, err = ValidateMemberGroupItems([]MemberGroupItem{
		{ChannelId: ch.Id, Model: "m-v"}, {ChannelId: ch.Id, Model: "m-v"},
	})
	assert.Error(t, err, "组内 (渠道, 模型) 重复必须拒绝")

	_, err = ValidateMemberGroupItems([]MemberGroupItem{{ChannelId: 99999, Model: "m-v"}})
	assert.Error(t, err, "组内成员渠道不存在必须拒绝")
}

// ListModelSummaries 按展开后口径计数：member_count / disabled_member_count 含组
// 展开成员（"组被关"与"成员被关"同计 disabled，routing-spec §1.4）。
func TestModelSummariesExpandedCount(t *testing.T) {
	setupGroupTest(t)
	ch := newTestChannel(t, "ch-cnt", "m-cnt")
	group := newTestGroup(t, "grp-cnt",
		MemberGroupItem{ChannelId: ch.Id, Model: "m-cnt", Priority: 1},
	)
	require.NoError(t, UpsertLane(&Lane{
		Name: "lane-cnt", Enabled: true, Mode: LaneModeFailover,
		Members: []LaneMember{{GroupId: group.Id, Priority: 1}},
	}))

	summaries, err := ListModelSummaries()
	require.NoError(t, err)
	var found *ModelSummary
	for i := range summaries {
		if summaries[i].Model == "lane-cnt" {
			found = &summaries[i]
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, 1, found.MemberCount, "member_count 按展开后成员数")

	saved, err := GetMemberGroupByName("grp-cnt")
	require.NoError(t, err)
	saved.Disabled = true
	require.NoError(t, UpsertMemberGroup(saved))
	summaries, err = ListModelSummaries()
	require.NoError(t, err)
	for i := range summaries {
		if summaries[i].Model == "lane-cnt" {
			found = &summaries[i]
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, 1, found.DisabledMemberCount, "组全局关计入 disabled_member_count")
	assert.Equal(t, 0, found.AvailableMemberCount, "组关后无可用成员")
}
