package model

import (
	"testing"

	"pbr/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetModels 在语义上是集合：去空白、丢空项、按首次出现去重。
// 基座把它当裸字符串切分，导致 models:[""] 会写进 abilities、重复声明会把 member_count 虚高。
func TestGetModelsTrimsDropsEmptyAndDedupes(t *testing.T) {
	channel := Channel{Models: " a , b ,,a , a "}
	assert.Equal(t, []string{"a", "b"}, channel.GetModels())
	empty := Channel{Models: ""}
	assert.Empty(t, empty.GetModels())
	blank := Channel{Models: " , , "}
	assert.Empty(t, blank.GetModels())
}

func TestAddAbilitiesSkipsEmptyModels(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Ability{}))
	require.NoError(t, DB.Where("channel_id = ?", 987654).Delete(&Ability{}).Error)
	channel := &Channel{Id: 987654, Models: "", Group: "default", Status: common.ChannelStatusEnabled}
	require.NoError(t, channel.AddAbilities(nil))
	var count int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", 987654).Count(&count).Error)
	assert.Zero(t, count, "空 models 不应生成空模型 ability")
}
