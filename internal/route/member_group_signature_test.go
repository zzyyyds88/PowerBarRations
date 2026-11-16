package route

import (
	"testing"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
)

// 成员组引入后的快照签名契约（routing-spec §1.3/§1.4、ADR 0009）：组配置的改动
// （组内成员、组内顺序、组开关、换引用的组）**不经过车道保存**，车道 UpdatedAt
// 不变——签名必须靠展开结果里的字段（Group/Model/Priority/Disabled/MemberId）
// 捕获这些变化，否则"改了组、快照不重建"会让选路继续用旧成员链。
//
// 下面每组用例都固定 LaneVersion（模拟"车道没重新保存"），只改组侧字段。

func groupFixture() *model.ResolvedRoute {
	return &model.ResolvedRoute{
		Source:      model.RouteSourceExplicit,
		Mode:        model.LaneModeFailover,
		RouteKey:    "lane-g",
		LaneVersion: 1000, // 固定：证明签名变化与车道版本无关
		Members: []model.RouteMember{
			{ChannelId: 1, Channel: "ch-a", Model: "m-1", UpstreamModel: "m-1", Priority: 2, MemberId: -11, Group: "grp"},
			{ChannelId: 2, Channel: "ch-b", Model: "m-2", UpstreamModel: "m-2", Priority: 2, MemberId: -12, Group: "grp"},
		},
	}
}

// 组全局/成员停用变化（展开成员 Disabled 变）必须改变签名。
func TestSignatureCatchesGroupDisableChange(t *testing.T) {
	before := routeSnapshotSignature(groupFixture())
	after := groupFixture()
	after.Members[0].Disabled = true // 组全局关或该成员被组内停用
	assert.NotEqual(t, before, routeSnapshotSignature(after),
		"组开关变化在车道 UpdatedAt 不变时必须触发快照重建")
}

// 组内顺序/成员增删（MemberId、成员集合变化）必须改变签名。
func TestSignatureCatchesGroupMembershipChange(t *testing.T) {
	before := routeSnapshotSignature(groupFixture())
	after := groupFixture()
	after.Members = after.Members[1:] // 组里删了一个成员
	assert.NotEqual(t, before, routeSnapshotSignature(after))

	reordered := groupFixture()
	reordered.Members[0], reordered.Members[1] = reordered.Members[1], reordered.Members[0]
	assert.NotEqual(t, before, routeSnapshotSignature(reordered),
		"组内顺序变化必须触发快照重建")
}

// 换引用的组（Group 字段变化，即使展开成员恰好相同）必须改变签名。
func TestSignatureCatchesGroupSwap(t *testing.T) {
	before := routeSnapshotSignature(groupFixture())
	after := groupFixture()
	after.Members[0].Group = "grp-other"
	after.Members[1].Group = "grp-other"
	assert.NotEqual(t, before, routeSnapshotSignature(after),
		"组名（来源组）显式入签名：换组引用必须触发快照重建")
}

// 车道版本与全部展开字段不变时签名稳定（避免无谓重建）。
func TestSignatureStableWithoutChange(t *testing.T) {
	assert.Equal(t, routeSnapshotSignature(groupFixture()), routeSnapshotSignature(groupFixture()))
}
