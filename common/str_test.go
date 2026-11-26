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

	// 中间一个坏字节不得把后面的合法内容一起丢掉。
	got = TruncateUTF8("a\xffb", 10)
	assert.Equal(t, "a\uFFFDb", got)
	assert.True(t, utf8.ValidString(got))
}

// 上游 transport 错误会带完整 URL（部分厂商 key 在 query 里），错误日志
// 必须先脱敏再截断；这里守卫 URL、域名与 key 三段都不能原文出现。
func TestMaskedLogPreviewMasksUpstreamTransportError(t *testing.T) {
	got := MaskedLogPreview(`Post "https://vendor.example.test/v1/chat?key=AIzaSyDsecret": dial tcp: connection refused`)
	assert.NotContains(t, got, "AIzaSyDsecret")
	assert.NotContains(t, got, "vendor.example.test")
	assert.Contains(t, got, "connection refused")
}
