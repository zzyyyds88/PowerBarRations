package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// GetModels 在语义上是集合：去空白、丢空项、按首次出现去重。
// 基座把它当裸字符串切分，导致 models:[""] 会生成幽灵声明、重复声明会把 member_count 虚高。
func TestGetModelsTrimsDropsEmptyAndDedupes(t *testing.T) {
	channel := Channel{Models: " a , b ,,a , a "}
	assert.Equal(t, []string{"a", "b"}, channel.GetModels())
	empty := Channel{Models: ""}
	assert.Empty(t, empty.GetModels())
	blank := Channel{Models: " , , "}
	assert.Empty(t, blank.GetModels())
}
