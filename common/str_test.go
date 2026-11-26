package common

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestTruncateUTF8KeepsValidBoundary(t *testing.T) {
	// 每个汉字 3 字节；上限 4 不能切出半个字符。
	got := TruncateUTF8("中文", 4)
	assert.Equal(t, "中", got)
	assert.True(t, utf8.ValidString(got))

	got = TruncateUTF8(strings.Repeat("a", 10), 4)
	assert.Equal(t, "aaaa", got)
	assert.Equal(t, "", TruncateUTF8("中", 0))
}
