package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"gorm.io/gorm"
)

func TestChannelValidateSettingsRejectsInvalidHTTPTransport(t *testing.T) {
	tests := []struct {
		name    string
		setting dto.ChannelSettings
		wantErr string
	}{
		{
			name:    "auto with shards is valid",
			setting: dto.ChannelSettings{HTTPProtocol: "auto", HTTP2ConnectionShards: 4},
		},
		{
			name:    "http1 with shards greater than one rejected",
			setting: dto.ChannelSettings{HTTPProtocol: "http1", HTTP2ConnectionShards: 2},
			wantErr: "http2_connection_shards",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{}
			channel.SetSetting(tt.setting)
			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestAdvancedCustomChannelRequiresModelListRouteOnlyWhenUpdateChecksEnabled(t *testing.T) {
	inferenceRoute := dto.AdvancedCustomRoute{
		IncomingPath: "/v1/chat/completions",
		UpstreamPath: "/v1/chat/completions",
		Converter:    "none",
	}

	tests := []struct {
		name          string
		checksEnabled bool
		routes        []dto.AdvancedCustomRoute
		wantErr       string
	}{
		{
			name: "legacy channel without discovery route remains valid",
			routes: []dto.AdvancedCustomRoute{
				inferenceRoute,
			},
		},
		{
			name:          "enabled checks require discovery route",
			checksEnabled: true,
			routes: []dto.AdvancedCustomRoute{
				inferenceRoute,
			},
			wantErr: dto.AdvancedCustomModelListPath,
		},
		{
			name:          "enabled checks accept discovery route",
			checksEnabled: true,
			routes: []dto.AdvancedCustomRoute{
				inferenceRoute,
				{
					IncomingPath: dto.AdvancedCustomModelListPath,
					UpstreamPath: dto.AdvancedCustomModelListPath,
					Converter:    "none",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{Type: constant.ChannelTypeAdvancedCustom}
			channel.SetOtherSettings(dto.ChannelOtherSettings{
				UpstreamModelUpdateCheckEnabled: tt.checksEnabled,
				AdvancedCustom: &dto.AdvancedCustomConfig{
					Routes: tt.routes,
				},
			})

			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseChannelSettingsIsPureAndDoesNotPersistBadConfig(t *testing.T) {
	truncateTables(t)
	raw := `{"proxy":"https://secret.example/?token=should-not-leak"`
	channel := &Channel{Name: "bad-config", Status: common.ChannelStatusEnabled, Setting: &raw}
	require.NoError(t, DB.Create(channel).Error)

	_, err := ParseChannelSettings(raw)
	require.Error(t, err)
	assert.Empty(t, channel.GetSetting().Proxy)
	assert.Empty(t, channel.GetOtherSettings().AdvancedCustom)
	configErr := channel.ConfigError()
	require.NotNil(t, configErr)
	assert.Equal(t, channelConfigErrorInvalidJSON, configErr.Code)
	assert.NotContains(t, configErr.Message, "secret.example")
	assert.NotContains(t, configErr.Message, "should-not-leak")

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	require.NotNil(t, stored.Setting)
	assert.Equal(t, raw, *stored.Setting, "getter 不得清空或写回坏配置")

	typed := &Channel{Setting: common.GetPointer(`{"pbr_rate_limits":[{"max_concurrency":"bad"}]}`)}
	typedErr := typed.ConfigError()
	require.NotNil(t, typedErr)
	assert.Equal(t, channelConfigErrorInvalidValue, typedErr.Code)
	assert.Contains(t, typedErr.Path, "pbr_rate_limits")
}

func TestExpandLaneChainWithVersionBatchLoadsChannels(t *testing.T) {
	truncateTables(t)
	first := &Channel{Name: "channel-a", Status: common.ChannelStatusEnabled, Models: "model-a"}
	second := &Channel{Name: "channel-b", Status: common.ChannelStatusEnabled, Models: "model-b"}
	require.NoError(t, DB.Create(first).Error)
	require.NoError(t, DB.Create(second).Error)
	group := &MemberGroup{Name: "group-a", Members: []MemberGroupItem{{ChannelId: second.Id, Model: "model-b"}}}
	require.NoError(t, DB.Create(group).Error)
	lane := &Lane{Name: "lane-a", Members: []LaneMember{
		{ChannelId: first.Id, Model: "model-a"},
		{GroupId: group.Id},
	}}

	queries := 0
	const callbackName = "test:count_expand_queries"
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callbackName, func(_ *gorm.DB) {
		queries++
	}))
	t.Cleanup(func() { DB.Callback().Query().Remove(callbackName) })

	members, _, err := expandLaneChainWithVersion(DB, lane)
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.Equal(t, 3, queries, "组、组成员、渠道各一次批量查询；不得逐成员查渠道")
	for i := range members {
		assert.NotNil(t, members[i].ChannelSnapshot)
		assert.Nil(t, members[i].ConfigError)
	}
}

func TestRouteMemberSnapshotCarriesRateLimitAndConfigError(t *testing.T) {
	truncateTables(t)
	raw := `{"pbr_rate_limits":[{"model":"model-a","max_concurrency":-1,"queue_wait_seconds":3}]}`
	channel := &Channel{Name: "channel-a", Status: common.ChannelStatusEnabled, Models: "model-a", Setting: &raw, UpdatedAt: 42}
	require.NoError(t, DB.Create(channel).Error)
	lane := &Lane{Name: "lane-a", Members: []LaneMember{{ChannelId: channel.Id, Model: "model-a"}}}

	members, _, err := expandLaneChainWithVersion(DB, lane)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.NotNil(t, members[0].ChannelSnapshot)
	assert.Equal(t, int64(42), members[0].ChannelVersion)
	require.NotNil(t, members[0].RateLimit)
	assert.Equal(t, -1, members[0].RateLimit.MaxConcurrency)
	assert.Nil(t, members[0].ConfigError)

	badRaw := `{"pbr_rate_limits":`
	require.NoError(t, DB.Model(channel).Update("setting", badRaw).Error)
	members, _, err = expandLaneChainWithVersion(DB, lane)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.NotNil(t, members[0].ConfigError)
	assert.False(t, strings.Contains(members[0].ConfigError.Message, "pbr_rate_limits"))
}
