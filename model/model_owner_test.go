package model

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
)

// abilities 表与渠道 priority/weight 均已物理删除：模型归属渠道类型直接由
// 启用渠道的声明推导，多渠道命中同一模型时按渠道 id 升序取第一个。

func clearPreferredOwnerTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
}

func insertPreferredOwnerCandidate(
	t *testing.T,
	channelID int,
	modelName string,
	group string,
	channelType int,
	channelStatus int,
) {
	t.Helper()
	require.NoError(t, DB.Create(&Channel{
		Id:     channelID,
		Type:   channelType,
		Key:    fmt.Sprintf("key-%d", channelID),
		Status: channelStatus,
		Name:   fmt.Sprintf("channel-%d", channelID),
		Group:  group,
		Models: modelName,
	}).Error)
}

func TestGetPreferredModelOwnerChannelTypes(t *testing.T) {
	const modelName = "gpt-5.4"

	tests := []struct {
		name     string
		setup    func(t *testing.T)
		groups   []string
		expected int
		found    bool
	}{
		{
			name: "openai only",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeOpenAI, common.ChannelStatusEnabled)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeOpenAI,
			found:    true,
		},
		{
			name: "codex only",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeCodex, common.ChannelStatusEnabled)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeCodex,
			found:    true,
		},
		{
			name: "lowest channel id wins on ties",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 2, modelName, "default", constant.ChannelTypeCodex, common.ChannelStatusEnabled)
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeOpenAI, common.ChannelStatusEnabled)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeOpenAI,
			found:    true,
		},
		{
			name: "group filter excludes other groups",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "vip", constant.ChannelTypeCodex, common.ChannelStatusEnabled)
				insertPreferredOwnerCandidate(t, 2, modelName, "default", constant.ChannelTypeOpenAI, common.ChannelStatusEnabled)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeOpenAI,
			found:    true,
		},
		{
			name: "disabled channels are ignored",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeCodex, common.ChannelStatusManuallyDisabled)
				insertPreferredOwnerCandidate(t, 2, modelName, "default", constant.ChannelTypeOpenAI, common.ChannelStatusAutoDisabled)
			},
			groups: []string{"default"},
			found:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearPreferredOwnerTables(t)
			tt.setup(t)

			owners, err := GetPreferredModelOwnerChannelTypes([]string{modelName}, tt.groups)
			require.NoError(t, err)

			got, ok := owners[modelName]
			require.Equal(t, tt.found, ok)
			if tt.found {
				require.Equal(t, tt.expected, got)
			}
		})
	}
}
