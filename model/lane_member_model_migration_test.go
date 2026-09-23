package model

// lane_members 模型语义迁移（ADR 0008）的行为保持测试。
//
// 迁移要保证：`effectiveUpstreamModel(channel, 迁移后的 Model)` 等于**迁移前的真名**。
// 三种存量情形逐条覆盖：真名命中映射右值 / 真名不是映射值 / 真名为空。

import (
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 构造"旧结构"的库：lane_members 只有 upstream_model 列，无 model 列。
func seedLegacyMemberSchema(t *testing.T) {
	t.Helper()
	setupLaneTest(t)
	// AutoMigrate 建的是新结构（含 model 列）。为了让迁移函数走到"重命名"分支，
	// 这里手工把列改回旧名，模拟升级前的库。
	require.NoError(t, DB.Exec("ALTER TABLE lane_members RENAME COLUMN model TO upstream_model").Error)
}

func TestMigrateLaneMemberModelSemanticsPreservesDerivedUpstream(t *testing.T) {
	seedLegacyMemberSchema(t)

	// 渠道映射：declared-model → vendor/real。
	mapping := `{"declared-model":"vendor/real"}`
	channel := &Channel{Name: "map-ch", Models: "declared-model", Status: common.ChannelStatusEnabled,
		Group: "default", Key: "sk", ModelMapping: &mapping}
	require.NoError(t, DB.Create(channel).Error)

	require.NoError(t, DB.Exec(`INSERT INTO lanes (name, mode, enabled, active_member, config, created_at, updated_at)
		VALUES ('lane-a', 'failover', 1, '', '{}', 0, 0)`).Error)
	var laneID int
	require.NoError(t, DB.Raw("SELECT id FROM lanes WHERE name = 'lane-a'").Scan(&laneID).Error)

	// 三种存量形态（用旧列名插入）：
	//  1. 真名 = 映射右值（加入成员时被物化写入的典型形态）→ 应回填成映射左键。
	//  2. 真名不是任何映射值（本就用模型名）→ 原值保留。
	//  3. 真名为空（旧语义"用渠道映射/路由键"）→ 回填车道路由键。
	insertLegacy := func(upstream string, priority int) {
		require.NoError(t, DB.Exec(`INSERT INTO lane_members
			(lane_id, channel_id, upstream_model, public_alias, priority, overrides, disabled)
			VALUES (?, ?, ?, '', ?, '', 0)`, laneID, channel.Id, upstream, priority).Error)
	}
	insertLegacy("vendor/real", 3) // 情形 1
	insertLegacy("plain-name", 2)  // 情形 2
	insertLegacy("", 1)            // 情形 3

	// 迁移前：记录每个成员的派生真名（旧口径下就是 upstream_model 本身或其回落值）。
	type want struct {
		model    string
		upstream string
	}
	// 情形 3 的旧语义是"回落 mapping[路由键]"：路由键 lane-a 无映射 → 回落 lane-a。
	expected := []want{
		{model: "declared-model", upstream: "vendor/real"},
		{model: "plain-name", upstream: "plain-name"},
		{model: "lane-a", upstream: "lane-a"},
	}

	require.NoError(t, migrateLaneMemberModelSemantics(DB))

	// 列已改名：新结构能正常读到 Model。
	lane, err := GetLaneByName("lane-a")
	require.NoError(t, err)
	require.Len(t, lane.Members, 3)

	// 按 priority 降序对齐插入顺序。
	byPriority := map[int]LaneMember{}
	for _, m := range lane.Members {
		byPriority[m.Priority] = m
	}
	for i, exp := range expected {
		priority := 3 - i
		got, ok := byPriority[priority]
		require.True(t, ok, "priority %d 的成员应存在", priority)
		assert.Equal(t, exp.model, got.Model, "priority %d 回填后的模型名", priority)
		// 行为保持的核心断言：用迁移后的 Model 推导真名，结果与迁移前一致。
		derived := effectiveUpstreamModel(channel, got.Model)
		assert.Equal(t, exp.upstream, derived,
			"priority %d 的派生真名必须与迁移前一致（行为保持）", priority)
	}
}

// 迁移幂等：已是新结构时直接返回，不报错、不改数据。
func TestMigrateLaneMemberModelSemanticsIsIdempotent(t *testing.T) {
	setupLaneTest(t)
	channel := newTestChannel(t, "idem-ch", "m-1")
	require.NoError(t, UpsertLane(&Lane{Name: "idem-lane", Enabled: true, Mode: LaneModeFailover,
		Members: []LaneMember{{ChannelId: channel.Id, Model: "m-1", Priority: 1}}}))

	// 连跑两次都不应出错。
	require.NoError(t, migrateLaneMemberModelSemantics(DB))
	require.NoError(t, migrateLaneMemberModelSemantics(DB))

	lane, err := GetLaneByName("idem-lane")
	require.NoError(t, err)
	require.Len(t, lane.Members, 1)
	assert.Equal(t, "m-1", lane.Members[0].Model)
}

// active_member 标签重写：`channel/真名` → `channel/模型`。
func TestMigrateLaneActiveMemberLabels(t *testing.T) {
	seedLegacyMemberSchema(t)
	mapping := `{"declared-model":"vendor/real"}`
	channel := &Channel{Name: "am-ch", Models: "declared-model", Status: common.ChannelStatusEnabled,
		Group: "default", Key: "sk", ModelMapping: &mapping}
	require.NoError(t, DB.Create(channel).Error)

	// 旧标签指向真名（vendor/real）。
	require.NoError(t, DB.Exec(`INSERT INTO lanes (name, mode, enabled, active_member, config, created_at, updated_at)
		VALUES ('am-lane', 'manual', 1, 'am-ch/vendor/real', '{}', 0, 0)`).Error)
	var laneID int
	require.NoError(t, DB.Raw("SELECT id FROM lanes WHERE name = 'am-lane'").Scan(&laneID).Error)
	require.NoError(t, DB.Exec(`INSERT INTO lane_members
		(lane_id, channel_id, upstream_model, public_alias, priority, overrides, disabled)
		VALUES (?, ?, 'vendor/real', '', 1, '', 0)`, laneID, channel.Id).Error)

	require.NoError(t, migrateLaneMemberModelSemantics(DB))

	var active string
	require.NoError(t, DB.Raw("SELECT active_member FROM lanes WHERE name = 'am-lane'").Scan(&active).Error)
	assert.Equal(t, "am-ch/declared-model", active, "标签应由真名重写为所选模型")
}

// 回归：`HasColumn` 的假阳性。
//
// 实测踩到：`db.Migrator().HasColumn(&LaneMember{}, "model")` 在"旧表只有
// upstream_model"时也返回 true（GORM 拿结构体的 column tag 去匹配），若用它判断
// 就会跳过重命名、迁移静默失败（真实库上升级后 model 列为空、旧列残留）。
// 本用例锁定"迁移必须真的重命名列"这一事实。
func TestMigrateRenamesLegacyColumnNotJustAddsNew(t *testing.T) {
	seedLegacyMemberSchema(t)
	channel := newTestChannel(t, "rename-ch", "m-1")
	require.NoError(t, DB.Exec(`INSERT INTO lanes (name, mode, enabled, active_member, config, created_at, updated_at)
		VALUES ('rename-lane', 'failover', 1, '', '{}', 0, 0)`).Error)
	var laneID int
	require.NoError(t, DB.Raw("SELECT id FROM lanes WHERE name = 'rename-lane'").Scan(&laneID).Error)
	require.NoError(t, DB.Exec(`INSERT INTO lane_members
		(lane_id, channel_id, upstream_model, public_alias, priority, overrides, disabled)
		VALUES (?, ?, 'm-1', '', 1, '', 0)`, laneID, channel.Id).Error)

	require.NoError(t, migrateLaneMemberModelSemantics(DB))

	// 旧列必须消失（不是"新旧并存"），否则读路径会拿到两套数据。
	columns, err := tableColumns(DB, "lane_members")
	require.NoError(t, err)
	assert.False(t, columns["upstream_model"], "旧列 upstream_model 必须已被重命名掉")
	assert.True(t, columns["model"], "新列 model 必须存在")
}

// 回归：两列并存的中间态（此前迁移失败留下的库）必须被收口——
// 把旧列数据搬到新列并删掉旧列，不能留下"新列空、旧列有值"导致成员模型名全丢。
func TestMigrateConsolidatesWhenBothColumnsExist(t *testing.T) {
	setupLaneTest(t)
	// 新结构建好后，手工造出"两列并存 + 新列为空、旧列有值"的中间态。
	require.NoError(t, DB.Exec("ALTER TABLE lane_members ADD COLUMN upstream_model TEXT").Error)
	channel := newTestChannel(t, "both-ch", "m-1")
	require.NoError(t, DB.Exec(`INSERT INTO lanes (name, mode, enabled, active_member, config, created_at, updated_at)
		VALUES ('both-lane', 'failover', 1, '', '{}', 0, 0)`).Error)
	var laneID int
	require.NoError(t, DB.Raw("SELECT id FROM lanes WHERE name = 'both-lane'").Scan(&laneID).Error)
	require.NoError(t, DB.Exec(`INSERT INTO lane_members
		(lane_id, channel_id, model, upstream_model, public_alias, priority, overrides, disabled)
		VALUES (?, ?, '', 'm-1', '', 1, '', 0)`, laneID, channel.Id).Error)

	require.NoError(t, migrateLaneMemberModelSemantics(DB))

	lane, err := GetLaneByName("both-lane")
	require.NoError(t, err)
	require.Len(t, lane.Members, 1)
	assert.Equal(t, "m-1", lane.Members[0].Model, "旧列数据必须被搬到新列，不能丢")
	columns, err := tableColumns(DB, "lane_members")
	require.NoError(t, err)
	assert.False(t, columns["upstream_model"], "收口后旧列必须被删掉")
}
