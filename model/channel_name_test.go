package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 渠道名是管理面与车道成员的唯一寻址键（api-spec §4.1）：
// 历史重名必须显式报错，不能静默取第一条。
func TestGetChannelByNameRejectsAmbiguousNames(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&Channel{Name: "dup-ch", Key: "sk", Status: 1, Group: "default"}).Error)
	require.NoError(t, DB.Create(&Channel{Name: "dup-ch", Key: "sk", Status: 1, Group: "default"}).Error)

	_, err := GetChannelByName("dup-ch")
	require.Error(t, err)
	var ambiguous *ChannelNameAmbiguousError
	require.ErrorAs(t, err, &ambiguous)
	assert.Equal(t, "dup-ch", ambiguous.Name)
	assert.Equal(t, 2, ambiguous.Count)
}

// 创建路径（批量添加、复制、导入）必须先过唯一性校验，而不是等 DB 唯一索引兜底。
func TestBatchInsertChannelsRejectsDuplicateNames(t *testing.T) {
	truncateTables(t)
	err := BatchInsertChannels([]Channel{
		{Name: "same-ch", Key: "k1", Status: 1},
		{Name: "same-ch", Key: "k2", Status: 1},
	})
	require.Error(t, err)
	var conflict *ChannelNameConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "duplicate", conflict.Reason)

	require.NoError(t, DB.Create(&Channel{Name: "taken-ch", Key: "k", Status: 1}).Error)
	err = BatchInsertChannels([]Channel{{Name: "taken-ch", Key: "k2", Status: 1}})
	require.Error(t, err)
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "taken", conflict.Reason)
}

// 改名同样受唯一性约束：不能把渠道改成另一个已存在的名字。
func TestChannelUpdateRejectsRenameToTakenName(t *testing.T) {
	truncateTables(t)
	first := &Channel{Name: "first-ch", Key: "k", Status: 1, Group: "default"}
	second := &Channel{Name: "second-ch", Key: "k", Status: 1, Group: "default"}
	require.NoError(t, DB.Create(first).Error)
	require.NoError(t, DB.Create(second).Error)

	second.Name = "first-ch"
	err := second.Update()
	require.Error(t, err)
	var conflict *ChannelNameConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "taken", conflict.Reason)
}
