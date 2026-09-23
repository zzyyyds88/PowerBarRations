package route

// 成员模型语义（ADR 0008）在选路层的口径：成员存"所选模型"、上游真名由渠道映射推导，
// 且**渠道映射改动必须触发快照重建**——映射改动不经过成员重插，只能靠签名变化发现。

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zzyyyds88/PowerBarRations/model"
)

// 快照签名必须对**派生上游真名**敏感：渠道 `model_mapping` 改动不经过成员重插
// （LaneVersion / MemberId 都不变），若真名不在签名里，改映射的车道会继续把请求
// 打到旧上游名，直到进程重启（routing-spec §1.3、ADR 0008）。
func TestSnapshotSignatureSensitiveToDerivedUpstreamModel(t *testing.T) {
	resolved := testRoute("lane-upstream-signature", 2, 2)
	before := routeSnapshotSignature(resolved)

	// 只改派生真名（模拟渠道映射从 vendor/v1 改成 vendor/v2）。
	resolved.Members[0].UpstreamModel = "vendor/v2"
	after := routeSnapshotSignature(resolved)

	assert.NotEqual(t, before, after,
		"只改派生上游真名也必须让快照签名变化，否则改渠道映射后仍打旧上游名")
}

// 签名必须对**所选模型**敏感：它是成员身份，改了就是另一个成员（转发目标随之变化）。
func TestSnapshotSignatureSensitiveToMemberModel(t *testing.T) {
	resolved := testRoute("lane-model-signature", 2, 2)
	before := routeSnapshotSignature(resolved)

	resolved.Members[0].Model = "another-model"
	after := routeSnapshotSignature(resolved)

	assert.NotEqual(t, before, after, "成员所选模型是身份，必须进签名")
}

// 成员标签用**所选模型**（成员身份），不随渠道映射改动漂移（ADR 0008）：
// 真名变了不该让 attempts 链里的成员换个名字。
func TestMemberLabelUsesSelectedModelNotDerivedUpstream(t *testing.T) {
	member := &model.RouteMember{
		ChannelId:     7,
		Channel:       "channel-a",
		Model:         "member-model",
		UpstreamModel: "vendor/real-name",
	}
	assert.Equal(t, "channel-a/member-model", memberLabel(member),
		"标签用成员所选模型（身份稳定），不用派生真名")
}
