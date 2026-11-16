package constant

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPath2RelayMode(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{path: "/v1/alpha/search", want: RelayModeAlphaSearch},
		{path: "/v1/alpha/search?foo=1", want: RelayModeAlphaSearch},
		// G5 入站端点：responses / images variations / moderations 必须是独立 mode，
		// 否则会落回 Unknown 或误判成其它图像/chat 语义。
		{path: "/v1/responses", want: RelayModeResponses},
		{path: "/v1/responses/compact", want: RelayModeResponsesCompact},
		{path: "/v1/images/variations", want: RelayModeImagesVariations},
		{path: "/v1/images/edits", want: RelayModeImagesEdits},
		{path: "/v1/images/generations", want: RelayModeImagesGenerations},
		{path: "/v1/moderations", want: RelayModeModerations},
		{path: "/v1/messages", want: RelayModeUnknown},
		{path: "/v1/messages/count_tokens", want: RelayModeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, Path2RelayMode(tt.path))
		})
	}
}

// RelayModeImagesVariations 追加在常量块末尾：既有 relay mode 的 iota 数值是
// 隐式契约（渠道适配器与日志按数值比较），插队会让后续模式静默错位。
func TestRelayModeImagesVariationsIsAppended(t *testing.T) {
	assert.Equal(t, RelayModeAlphaSearch+1, RelayModeImagesVariations)
}
