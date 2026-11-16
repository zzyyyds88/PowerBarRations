package async

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/constant"
)

func TestIsOpenAICompatibleChannelType(t *testing.T) {
	for _, channelType := range []int{
		constant.ChannelTypeOpenAI,
		constant.ChannelTypeCustom,
		constant.ChannelTypeAdvancedCustom,
		// 适配器就是 OpenAI 协议的渠道：OpenAIMax / OpenRouter / Xinference。
		constant.ChannelTypeOpenAIMax,
		constant.ChannelTypeOpenRouter,
		constant.ChannelTypeXinference,
	} {
		require.True(t, isOpenAICompatibleChannelType(channelType), "channel type %d", channelType)
	}
	for _, channelType := range []int{
		constant.ChannelTypeAnthropic,
		constant.ChannelTypeGemini,
		constant.ChannelTypeAws,
		constant.ChannelTypeCohere,
	} {
		require.False(t, isOpenAICompatibleChannelType(channelType), "channel type %d", channelType)
	}
}
