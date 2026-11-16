package dto

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

func TestAdvancedCustomValidateResponsesToChatConverterPath(t *testing.T) {
	valid := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    advancedCustomConverterOpenAIResponsesToOpenAIChat,
			},
		},
	}
	require.NoError(t, valid.Validate())

	validGemini := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    advancedCustomConverterOpenAIResponsesToGemini,
			},
		},
	}
	require.NoError(t, validGemini.Validate())

	tests := []struct {
		name         string
		incomingPath string
	}{
		{name: "chat completions", incomingPath: "/v1/chat/completions"},
		{name: "responses compact", incomingPath: "/v1/responses/compact"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AdvancedCustomConfig{
				Routes: []AdvancedCustomRoute{
					{
						IncomingPath: tt.incomingPath,
						UpstreamPath: "/v1/chat/completions",
						Converter:    advancedCustomConverterOpenAIResponsesToOpenAIChat,
					},
				},
			}
			err := config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "converter does not match incoming_path")
		})
	}
}

func TestAdvancedCustomValidateModelListRouteConstraints(t *testing.T) {
	valid := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: AdvancedCustomModelListPath,
				UpstreamPath: "https://upstream.example/custom/models",
				Converter:    advancedCustomConverterNone,
			},
		},
	}
	require.NoError(t, valid.Validate())

	tests := []struct {
		name   string
		routes []AdvancedCustomRoute
		want   string
	}{
		{
			name: "model matching rules",
			routes: []AdvancedCustomRoute{
				{
					IncomingPath: AdvancedCustomModelListPath,
					UpstreamPath: "/v1/models",
					Models:       []string{"gpt-4o"},
				},
			},
			want: "models must be empty",
		},
		{
			name: "converter",
			routes: []AdvancedCustomRoute{
				{
					IncomingPath: AdvancedCustomModelListPath,
					UpstreamPath: "/v1/models",
					Converter:    advancedCustomConverterOpenAIChatToOpenAIResponses,
				},
			},
			want: "converter must be none",
		},
		{
			name: "model placeholder",
			routes: []AdvancedCustomRoute{
				{
					IncomingPath: AdvancedCustomModelListPath,
					UpstreamPath: "/v1/models/{model}",
				},
			},
			want: "upstream_path must not contain {model}",
		},
		{
			name: "duplicate routes",
			routes: []AdvancedCustomRoute{
				{IncomingPath: AdvancedCustomModelListPath, UpstreamPath: "/v1/models"},
				{IncomingPath: AdvancedCustomModelListPath, UpstreamPath: "/provider/models"},
			},
			want: "duplicates the /v1/models route",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&AdvancedCustomConfig{Routes: tt.routes}).Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestAdvancedCustomModelListRouteRequiresExactIncomingPath(t *testing.T) {
	config := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/{model}",
				UpstreamPath: "/generic/{model}",
			},
			{
				IncomingPath: AdvancedCustomModelListPath,
				UpstreamPath: "/provider/models",
			},
		},
	}
	require.NoError(t, config.Validate())

	route, ok := config.ModelListRoute()
	require.True(t, ok)
	assert.Equal(t, "/provider/models", route.UpstreamPath)
}

func TestAdvancedCustomValidateBalanceRouteConstraints(t *testing.T) {
	valid := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{{
			IncomingPath: AdvancedCustomBalancePath,
			UpstreamPath: "/provider/balance",
			Converter:    advancedCustomConverterNone,
		}},
	}
	require.NoError(t, valid.Validate())

	route, ok := valid.BalanceRoute()
	require.True(t, ok)
	assert.Equal(t, "/provider/balance", route.UpstreamPath)

	tests := []struct {
		name   string
		routes []AdvancedCustomRoute
		want   string
	}{
		{
			name: "model matching rules",
			routes: []AdvancedCustomRoute{{
				IncomingPath: AdvancedCustomBalancePath,
				UpstreamPath: "/provider/balance",
				Models:       []string{"gpt-4o"},
			}},
			want: "models must be empty",
		},
		{
			name: "converter",
			routes: []AdvancedCustomRoute{{
				IncomingPath: AdvancedCustomBalancePath,
				UpstreamPath: "/provider/balance",
				Converter:    advancedCustomConverterOpenAIChatToOpenAIResponses,
			}},
			want: "converter must be none",
		},
		{
			name: "model placeholder",
			routes: []AdvancedCustomRoute{{
				IncomingPath: AdvancedCustomBalancePath,
				UpstreamPath: "/provider/{model}/balance",
			}},
			want: "upstream_path must not contain {model}",
		},
		{
			name: "duplicate routes",
			routes: []AdvancedCustomRoute{
				{IncomingPath: AdvancedCustomBalancePath, UpstreamPath: "/provider/balance"},
				{IncomingPath: AdvancedCustomBalancePath, UpstreamPath: "/provider/credits"},
			},
			want: "duplicates the /v1/dashboard/billing/credit_grants route",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&AdvancedCustomConfig{Routes: tt.routes}).Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestAdvancedCustomValidateDuplicateIncomingPathWithDisjointModels(t *testing.T) {
	config := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    advancedCustomConverterOpenAIResponsesToOpenAIChat,
				Models:       []string{"gpt-4o"},
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    advancedCustomConverterOpenAIResponsesToGemini,
				Models:       []string{"gemini-2.5-flash"},
			},
		},
	}

	require.NoError(t, config.Validate())
}

func TestAdvancedCustomValidateDuplicateIncomingPathRejectsOverlappingModels(t *testing.T) {
	config := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    advancedCustomConverterOpenAIResponsesToOpenAIChat,
				Models:       []string{"shared-model"},
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    advancedCustomConverterOpenAIResponsesToGemini,
				Models:       []string{"shared-model"},
			},
		},
	}

	err := config.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "models overlaps")
}

func TestAdvancedCustomValidateDuplicateIncomingPathRejectsMultipleCatchAllRoutes(t *testing.T) {
	config := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    advancedCustomConverterOpenAIResponsesToOpenAIChat,
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    advancedCustomConverterOpenAIResponsesToGemini,
			},
		},
	}

	err := config.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catch-all already exists")
}

func TestAdvancedCustomValidateDuplicateIncomingPathRequiresCatchAllLast(t *testing.T) {
	config := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    advancedCustomConverterOpenAIResponsesToOpenAIChat,
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    advancedCustomConverterOpenAIResponsesToGemini,
				Models:       []string{"gemini-2.5-flash"},
			},
		},
	}

	err := config.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catch-all route must be last")
}

func TestAdvancedCustomMatchPathForModel(t *testing.T) {
	config := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    advancedCustomConverterOpenAIResponsesToGemini,
				Models:       []string{"gemini-2.5-flash"},
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    advancedCustomConverterOpenAIResponsesToOpenAIChat,
				Models:       []string{"gpt-4o"},
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/responses",
				Converter:    advancedCustomConverterNone,
			},
		},
	}
	require.NoError(t, config.Validate())

	geminiRoute, ok := config.MatchPathForModel("/v1/responses", "gemini-2.5-flash")
	require.True(t, ok)
	assert.Equal(t, advancedCustomConverterOpenAIResponsesToGemini, geminiRoute.Converter)

	chatRoute, ok := config.MatchPathForModel("/v1/responses", "gpt-4o")
	require.True(t, ok)
	assert.Equal(t, advancedCustomConverterOpenAIResponsesToOpenAIChat, chatRoute.Converter)

	fallbackRoute, ok := config.MatchPathForModel("/v1/responses", "unknown-model")
	require.True(t, ok)
	assert.Equal(t, advancedCustomConverterNone, fallbackRoute.Converter)
}

func TestAdvancedCustomMatchPathForModelRegexRules(t *testing.T) {
	config := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    advancedCustomConverterOpenAIResponsesToGemini,
				Models:       []string{"re:^gemini-"},
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    advancedCustomConverterOpenAIResponsesToOpenAIChat,
				Models:       []string{"re:(?i)^OAI-"},
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/responses",
				Converter:    advancedCustomConverterNone,
			},
		},
	}
	require.NoError(t, config.Validate())

	geminiRoute, ok := config.MatchPathForModel("/v1/responses", "gemini-2.5-flash")
	require.True(t, ok)
	assert.Equal(t, advancedCustomConverterOpenAIResponsesToGemini, geminiRoute.Converter)

	chatRoute, ok := config.MatchPathForModel("/v1/responses", "oai-test")
	require.True(t, ok)
	assert.Equal(t, advancedCustomConverterOpenAIResponsesToOpenAIChat, chatRoute.Converter)

	fallbackRoute, ok := config.MatchPathForModel("/v1/responses", "gpt-4o")
	require.True(t, ok)
	assert.Equal(t, advancedCustomConverterNone, fallbackRoute.Converter)
}

func TestAdvancedCustomRouteModelRegexRulesAreCachedCompiled(t *testing.T) {
	require.True(t, matchAdvancedCustomRouteModelRule("re:^cache-probe-", "cache-probe-model"))

	cached, ok := advancedCustomModelRegexCache.Load("^cache-probe-")
	require.True(t, ok)
	require.NotNil(t, cached)
	_, isRegexp := cached.(*regexp.Regexp)
	require.True(t, isRegexp)

	// Invalid patterns never match and are cached as nil so they are not recompiled.
	require.False(t, matchAdvancedCustomRouteModelRule("re:(", "anything"))
	cached, ok = advancedCustomModelRegexCache.Load("(")
	require.True(t, ok)
	re, _ := cached.(*regexp.Regexp)
	require.Nil(t, re)

	// Cached entries keep matching correctly on subsequent calls.
	require.True(t, matchAdvancedCustomRouteModelRule("re:^cache-probe-", "cache-probe-other"))
	require.False(t, matchAdvancedCustomRouteModelRule("re:^cache-probe-", "other-model"))
}

func TestAdvancedCustomMatchPathForModelExactRuleDoesNotMatchPrefix(t *testing.T) {
	config := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    advancedCustomConverterOpenAIResponsesToGemini,
				Models:       []string{"gemini"},
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/responses",
				Converter:    advancedCustomConverterNone,
			},
		},
	}
	require.NoError(t, config.Validate())

	fallbackRoute, ok := config.MatchPathForModel("/v1/responses", "gemini-2.5-flash")
	require.True(t, ok)
	assert.Equal(t, advancedCustomConverterNone, fallbackRoute.Converter)
}

func TestAdvancedCustomValidateDuplicateIncomingPathRejectsInvalidRegexModels(t *testing.T) {
	tests := []struct {
		name   string
		models []string
		want   string
	}{
		{name: "empty regex", models: []string{"re:"}, want: "regex is empty"},
		{name: "invalid regex", models: []string{"re:["}, want: "regex is invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AdvancedCustomConfig{
				Routes: []AdvancedCustomRoute{
					{
						IncomingPath: "/v1/responses",
						UpstreamPath: "/v1beta/models/{model}:generateContent",
						Converter:    advancedCustomConverterOpenAIResponsesToGemini,
						Models:       tt.models,
					},
				},
			}

			err := config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestAdvancedCustomValidateDuplicateIncomingPathRejectsDuplicateRegexModels(t *testing.T) {
	config := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    advancedCustomConverterOpenAIResponsesToGemini,
				Models:       []string{"re:^gemini-"},
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    advancedCustomConverterOpenAIResponsesToOpenAIChat,
				Models:       []string{"re:^gemini-"},
			},
		},
	}

	err := config.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "models overlaps")
}

func TestAdvancedCustomMatchPathForModelUsesFirstMatchingRegexRoute(t *testing.T) {
	config := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    advancedCustomConverterOpenAIResponsesToGemini,
				Models:       []string{"re:^gemini-"},
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    advancedCustomConverterOpenAIResponsesToOpenAIChat,
				Models:       []string{"gemini-2.5-flash"},
			},
		},
	}
	require.NoError(t, config.Validate())

	route, ok := config.MatchPathForModel("/v1/responses", "gemini-2.5-flash")
	require.True(t, ok)
	assert.Equal(t, advancedCustomConverterOpenAIResponsesToGemini, route.Converter)
}

func TestAdvancedCustomSupportedEndpointTypesForModel(t *testing.T) {
	config := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    advancedCustomConverterOpenAIResponsesToGemini,
				Models:       []string{"re:^gemini-"},
			},
			{
				IncomingPath: "/v1beta/models/{model}:generateContent",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Models:       []string{"re:^gemini-"},
			},
			{
				IncomingPath: "/v1beta/models/{model}:streamGenerateContent",
				UpstreamPath: "/v1beta/models/{model}:streamGenerateContent",
				Models:       []string{"re:^gemini-"},
			},
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "/v1/chat/completions",
				Models:       []string{"gpt-4o"},
			},
			{
				IncomingPath: "/v1/messages",
				UpstreamPath: "/v1/messages",
			},
			{
				IncomingPath: "/custom/endpoint",
				UpstreamPath: "/custom/endpoint",
			},
		},
	}
	require.NoError(t, config.Validate())

	assert.Equal(t, []types.EndpointType{
		types.EndpointTypeOpenAIResponse,
		types.EndpointTypeGemini,
		types.EndpointTypeAnthropic,
	}, config.SupportedEndpointTypesForModel("gemini-2.5-flash"))
	assert.Equal(t, []types.EndpointType{
		types.EndpointTypeOpenAI,
		types.EndpointTypeAnthropic,
	}, config.SupportedEndpointTypesForModel("gpt-4o"))
	assert.Equal(t, []types.EndpointType{
		types.EndpointTypeAnthropic,
	}, config.SupportedEndpointTypesForModel("other-model"))
}

func TestAdvancedCustomValidateAlphaSearchConverterPath(t *testing.T) {
	valid := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/alpha/search",
				UpstreamPath: "/v1/alpha/search",
				Converter:    advancedCustomConverterNone,
			},
		},
	}
	require.NoError(t, valid.Validate())
	assert.Equal(t, []types.EndpointType{
		types.EndpointTypeOpenAIAlphaSearch,
	}, valid.SupportedEndpointTypesForModel("gpt-5.1"))

	nonNoneConverters := []string{
		advancedCustomConverterClaudeMessagesToOpenAIChat,
		advancedCustomConverterOpenAIChatToClaudeMessages,
		advancedCustomConverterOpenAIChatToOpenAIResponses,
		advancedCustomConverterOpenAIResponsesToOpenAIChat,
		advancedCustomConverterOpenAIResponsesToGemini,
		advancedCustomConverterGeminiContentToOpenAIChat,
		advancedCustomConverterOpenAIChatToGeminiContent,
	}
	for _, converter := range nonNoneConverters {
		t.Run(converter, func(t *testing.T) {
			config := &AdvancedCustomConfig{
				Routes: []AdvancedCustomRoute{
					{
						IncomingPath: "/v1/alpha/search",
						UpstreamPath: "/v1/alpha/search",
						Converter:    converter,
					},
				},
			}
			err := config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "converter does not match incoming_path")
		})
	}
}

func TestChannelSettingsHTTPTransportJSONRoundTrip(t *testing.T) {
	legacy := `{"proxy":"http://127.0.0.1:8080","force_format":true}`
	var settings ChannelSettings
	require.NoError(t, json.Unmarshal([]byte(legacy), &settings))
	assert.Equal(t, "http://127.0.0.1:8080", settings.Proxy)
	assert.True(t, settings.ForceFormat)
	assert.Empty(t, settings.HTTPProtocol)
	assert.Zero(t, settings.HTTP2ConnectionShards)

	encoded, err := json.Marshal(settings)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "http_protocol")
	assert.NotContains(t, string(encoded), "http2_connection_shards")

	explicit := ChannelSettings{
		Proxy:                 "socks5://127.0.0.1:1080",
		HTTPProtocol:          HTTPProtocolHTTP1,
		HTTP2ConnectionShards: 1,
	}
	encoded, err = json.Marshal(explicit)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"http_protocol":"http1"`)

	var decoded ChannelSettings
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, explicit.HTTPProtocol, decoded.HTTPProtocol)
	assert.Equal(t, 1, decoded.HTTP2ConnectionShards)

	sharded := ChannelSettings{HTTP2ConnectionShards: 4}
	encoded, err = json.Marshal(sharded)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"http2_connection_shards":4`)
	assert.NotContains(t, string(encoded), "http_protocol")
}

func TestChannelSettingsValidateHTTPTransport(t *testing.T) {
	require.NoError(t, (&ChannelSettings{}).ValidateHTTPTransport())
	require.NoError(t, (&ChannelSettings{HTTPProtocol: "AUTO"}).ValidateHTTPTransport())
	require.NoError(t, (&ChannelSettings{HTTPProtocol: "http1"}).ValidateHTTPTransport())
	require.NoError(t, (&ChannelSettings{HTTP2ConnectionShards: 8}).ValidateHTTPTransport())

	err := (&ChannelSettings{HTTPProtocol: "http2"}).ValidateHTTPTransport()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http_protocol")

	err = (&ChannelSettings{HTTP2ConnectionShards: -1}).ValidateHTTPTransport()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http2_connection_shards")

	err = (&ChannelSettings{HTTP2ConnectionShards: 9}).ValidateHTTPTransport()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http2_connection_shards")

	err = (&ChannelSettings{HTTPProtocol: "http1", HTTP2ConnectionShards: 2}).ValidateHTTPTransport()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http2_connection_shards")
}

func TestChannelOtherSettingsValidateToolLossPolicy(t *testing.T) {
	require.NoError(t, (*ChannelOtherSettings)(nil).ValidateToolLossPolicy())
	require.NoError(t, (&ChannelOtherSettings{}).ValidateToolLossPolicy())
	require.NoError(t, (&ChannelOtherSettings{ToolLossPolicy: "allow"}).ValidateToolLossPolicy())
	require.NoError(t, (&ChannelOtherSettings{ToolLossPolicy: "safe"}).ValidateToolLossPolicy())
	require.NoError(t, (&ChannelOtherSettings{ToolLossPolicy: "strict"}).ValidateToolLossPolicy())

	err := (&ChannelOtherSettings{ToolLossPolicy: "drop"}).ValidateToolLossPolicy()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool_loss_policy")
}

// 峰谷价格契约（design-v1 §16.9#7，api-spec §4.1）：days 1–7、HH:MM 左闭右开、
// start > end 跨午夜、每行 ≤64 条、timezone 必须可解析、band 单价非负。

func priceBandFloat(value float64) *float64 { return &value }

func TestParseClockHHMM(t *testing.T) {
	tests := []struct {
		value   string
		minutes int
		ok      bool
	}{
		{value: "00:00", minutes: 0, ok: true},
		{value: "09:05", minutes: 9*60 + 5, ok: true},
		{value: "23:59", minutes: 23*60 + 59, ok: true},
		{value: " 08:30 ", minutes: 8*60 + 30, ok: true},
		{value: "", ok: false},
		{value: "8:00", ok: false},
		{value: "08:0", ok: false},
		{value: "24:00", ok: false},
		{value: "08:60", ok: false},
		{value: "08-00", ok: false},
		{value: "+8:00", ok: false},
	}
	for _, tt := range tests {
		minutes, ok := ParseClockHHMM(tt.value)
		assert.Equal(t, tt.ok, ok, tt.value)
		if tt.ok {
			assert.Equal(t, tt.minutes, minutes, tt.value)
		}
	}
}

func TestChannelModelPriceValidateAcceptsBands(t *testing.T) {
	price := &ChannelModelPrice{
		Model: "model-peak", Input: 1, Output: 2,
		Timezone: "Asia/Shanghai",
		Bands: []PriceBand{
			{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00", Input: priceBandFloat(5), Output: priceBandFloat(10)},
			{Days: []int{6, 7}, Start: "22:00", End: "02:00", Input: priceBandFloat(0)},
		},
	}
	require.NoError(t, price.Validate())

	// 未填 timezone 也合法（默认 Asia/Shanghai）；无 bands 的旧单层价同样合法。
	require.NoError(t, (&ChannelModelPrice{Model: "legacy", Input: 1}).Validate())
}

func TestChannelModelPriceValidateRejectsInvalidBands(t *testing.T) {
	valid := func() *ChannelModelPrice {
		return &ChannelModelPrice{
			Model: "m",
			Bands: []PriceBand{{Days: []int{1}, Start: "09:00", End: "18:00", Input: priceBandFloat(5)}},
		}
	}

	tooManyBands := make([]PriceBand, MaxPriceBandsPerModel+1)
	for i := range tooManyBands {
		tooManyBands[i] = PriceBand{Days: []int{1}, Start: "09:00", End: "18:00"}
	}

	tests := []struct {
		name   string
		mutate func(*ChannelModelPrice)
		want   string
	}{
		{name: "empty model", mutate: func(p *ChannelModelPrice) { p.Model = "  " }, want: "model name must not be empty"},
		{name: "negative row price", mutate: func(p *ChannelModelPrice) { p.Input = -1 }, want: "unit price must be >= 0"},
		{name: "too many bands", mutate: func(p *ChannelModelPrice) { p.Bands = tooManyBands }, want: "bands must not exceed 64 entries"},
		{name: "invalid timezone", mutate: func(p *ChannelModelPrice) { p.Timezone = "Mars/Olympus" }, want: "timezone"},
		{name: "empty days", mutate: func(p *ChannelModelPrice) { p.Bands[0].Days = nil }, want: "days must not be empty"},
		{name: "day below range", mutate: func(p *ChannelModelPrice) { p.Bands[0].Days = []int{0} }, want: "out of range 1-7"},
		{name: "day above range", mutate: func(p *ChannelModelPrice) { p.Bands[0].Days = []int{8} }, want: "out of range 1-7"},
		{name: "bad start", mutate: func(p *ChannelModelPrice) { p.Bands[0].Start = "9:00" }, want: `start "9:00" must be HH:MM`},
		{name: "bad end", mutate: func(p *ChannelModelPrice) { p.Bands[0].End = "18:60" }, want: "end"},
		{name: "empty range", mutate: func(p *ChannelModelPrice) { p.Bands[0].Start = "10:00"; p.Bands[0].End = "10:00" }, want: "start and end must differ"},
		{name: "negative band price", mutate: func(p *ChannelModelPrice) { p.Bands[0].Output = priceBandFloat(-0.1) }, want: "output must be >= 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price := valid()
			tt.mutate(price)
			err := price.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// 显式 0 与"未填"必须可区分：JSON 里 input:0 反序列化后是指针非 nil。
func TestPriceBandJSONKeepsExplicitZero(t *testing.T) {
	var price ChannelModelPrice
	require.NoError(t, json.Unmarshal([]byte(`{
		"model":"m","timezone":"Asia/Shanghai",
		"bands":[{"days":[1],"start":"00:00","end":"08:00","input":0,"output":2}]
	}`), &price))
	require.Len(t, price.Bands, 1)
	require.NotNil(t, price.Bands[0].Input, "显式 0 不能退化成未填")
	assert.InDelta(t, 0, *price.Bands[0].Input, 1e-9)
	require.NotNil(t, price.Bands[0].Output)
	assert.Nil(t, price.Bands[0].CacheRead)

	encoded, err := json.Marshal(price)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"input":0`)

	// 省略 timezone/bands 的旧单层价保持可解析。
	var legacy ChannelModelPrice
	require.NoError(t, json.Unmarshal([]byte(`{"model":"legacy","input":1}`), &legacy))
	assert.Empty(t, legacy.Timezone)
	assert.Empty(t, legacy.Bands)
	require.NoError(t, legacy.Validate())
}
