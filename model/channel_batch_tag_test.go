package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
)

// BatchSetChannelTag 在事务内批量更新渠道标签。
//
// 回归：abilities 同步链删除前，事务中间曾走全局 DB 查询，SQLite 单连接下
// 事务持写锁 + 全局连接再查询 = 自锁，请求永久挂起（控制台表现为"卡住无响应"）。
// 这里用超时守住：若再出现事务内跨连接查询，测试会超时失败而不是无限等待。
func TestBatchSetChannelTagDoesNotSelfLock(t *testing.T) {
	setupChannelStatusTest(t)

	first := &Channel{Name: "batch-tag-a", Key: "k1", Status: common.ChannelStatusEnabled, Group: "default", Models: "m1"}
	second := &Channel{Name: "batch-tag-b", Key: "k2", Status: common.ChannelStatusEnabled, Group: "default", Models: "m2"}
	require.NoError(t, DB.Create(first).Error)
	require.NoError(t, DB.Create(second).Error)

	done := make(chan error, 1)
	tag := "pooled"
	go func() { done <- BatchSetChannelTag([]int{first.Id, second.Id}, &tag) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("BatchSetChannelTag 自锁：10s 内未返回")
	}

	var saved Channel
	require.NoError(t, DB.Where("id = ?", first.Id).First(&saved).Error)
	require.NotNil(t, saved.Tag)
	assert.Equal(t, "pooled", *saved.Tag)

	var savedSecond Channel
	require.NoError(t, DB.Where("id = ?", second.Id).First(&savedSecond).Error)
	require.NotNil(t, savedSecond.Tag)
	assert.Equal(t, "pooled", *savedSecond.Tag, "同批渠道都应带上新标签")
}
