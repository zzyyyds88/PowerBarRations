package openai

import (
	"testing"

	"pbr/constant"
	relaycommon "pbr/relay/common"
	relayconstant "pbr/relay/constant"
	"github.com/stretchr/testify/assert"
)

// Custom（type 8）渠道 GetRequestURL 语义（ui-spec §6.4）：
//   - {model} 模板：整串替换后直用，不做任何追加；
//   - 原始 URL 不含 {model} 且路径以版本段（/v1、/v1beta、/openai，可带尾斜杠）
//     结尾：自动追加 /chat/completions（追加在 path 与 query 之间）；
//   - 其余情况（完整端点 URL、其他路径结尾）：维持旧语义原样直用。
func TestCustomGetRequestURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		model   string
		want    string
	}{
		{
			name:    "model template used as-is",
			baseURL: "https://host/{model}",
			model:   "m1",
			want:    "https://host/m1",
		},
		{
			name:    "model template inside path used as-is",
			baseURL: "https://host/v1/{model}/completions",
			model:   "m1",
			want:    "https://host/v1/m1/completions",
		},
		{
			name:    "v1 suffix gets chat completions appended",
			baseURL: "https://host/v1",
			model:   "m1",
			want:    "https://host/v1/chat/completions",
		},
		{
			name:    "v1 trailing slash gets chat completions appended",
			baseURL: "https://host/v1/",
			model:   "m1",
			want:    "https://host/v1/chat/completions",
		},
		{
			name:    "v1beta suffix gets chat completions appended",
			baseURL: "https://host/v1beta",
			model:   "m1",
			want:    "https://host/v1beta/chat/completions",
		},
		{
			name:    "openai suffix gets chat completions appended",
			baseURL: "https://host/openai/",
			model:   "m1",
			want:    "https://host/openai/chat/completions",
		},
		{
			name:    "complete endpoint URL untouched",
			baseURL: "https://host/v1/chat/completions",
			model:   "m1",
			want:    "https://host/v1/chat/completions",
		},
		{
			name:    "complete endpoint URL with trailing slash untouched",
			baseURL: "https://host/v1/chat/completions/",
			model:   "m1",
			want:    "https://host/v1/chat/completions/",
		},
		{
			name:    "non-version path suffix untouched",
			baseURL: "https://host/api/foo",
			model:   "m1",
			want:    "https://host/api/foo",
		},
		{
			name:    "bare host without path untouched",
			baseURL: "https://host",
			model:   "m1",
			want:    "https://host",
		},
		{
			name:    "query preserved and append lands between path and query",
			baseURL: "https://host/v1?api-key=abc",
			model:   "m1",
			want:    "https://host/v1/chat/completions?api-key=abc",
		},
		{
			name:    "complete URL with query untouched",
			baseURL: "https://host/v1/chat/completions?x=1",
			model:   "m1",
			want:    "https://host/v1/chat/completions?x=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adaptor := &Adaptor{}
			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeUnknown,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:       constant.ChannelTypeCustom,
					ChannelBaseUrl:    tt.baseURL,
					UpstreamModelName: tt.model,
				},
			}
			got, err := adaptor.GetRequestURL(info)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
