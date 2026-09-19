package model

import (
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 渠道 priority/weight 列已在 W1 物理删除（channel_priority_weight_migration.go）。
// 任何仍引用该列的查询都会在真机上抛 "no such column: priority"——"按标签搜索"
// 与"按类型列渠道"这两条路径曾各留一处写死的 "priority desc"，属于单测不覆盖、
// 只有真机才暴露的缺陷。这里锁住这些查询能正常执行。
func setupChannelQueryTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	memoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = memoryCacheEnabled
	})
}

func TestChannelQueriesDoNotReferenceDroppedPriorityColumn(t *testing.T) {
	setupChannelQueryTest(t)

	tag := "alpha"
	require.NoError(t, DB.Create(&Channel{
		Name: "tagged", Key: "k1", Status: common.ChannelStatusEnabled,
		Models: "m1", Group: "default", Tag: &tag,
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Name: "untagged", Key: "k2", Status: common.ChannelStatusEnabled,
		Models: "m1", Group: "default",
	}).Error)

	// SearchTags 曾写死 order := "priority desc"：列删除后此调用会报错。
	tags, err := SearchTags("tagged", "", "", false)
	require.NoError(t, err, "SearchTags 不得引用已删除的 channels.priority")
	require.Len(t, tags, 1)
	assert.Equal(t, "alpha", *tags[0])

	// 带 idSort=true 的分支同样不得引用旧列。
	_, err = SearchTags("tagged", "", "", true)
	require.NoError(t, err)

	// GetChannelsByType 曾写死 order := "priority desc"。
	channels, err := GetChannelsByType(0, 10, false, 0)
	require.NoError(t, err, "GetChannelsByType 不得引用已删除的 channels.priority")
	assert.Len(t, channels, 2)
}
