package model

import (
	"strings"
	"testing"

	"pbr/common"
	"pbr/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// W1 路由核心验收：模型名即路由键（docs/routing-spec-v1.md §1.1）。

func setupLaneTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM lane_members").Error)
	require.NoError(t, DB.Exec("DELETE FROM lanes").Error)

	memoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = memoryCacheEnabled })
}

func newTestChannel(t *testing.T, name string, priority int, models ...string) *Channel {
	t.Helper()
	priority64 := int64(priority)
	channel := &Channel{
		Name:     name,
		Models:   strings.Join(models, ","),
		Priority: &priority64,
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Key:      "sk-test",
	}
	require.NoError(t, DB.Create(channel).Error)
	return channel
}

func memberChannels(route *ResolvedRoute) []string {
	out := make([]string, 0, len(route.Members))
	for _, m := range route.Members {
		out = append(out, m.Channel)
	}
	return out
}

// 渠道声明同一模型名 → 按渠道 priority 降序自动成链；未声明的模型为空链。
func TestImplicitChainOrdersByPriorityDesc(t *testing.T) {
	setupLaneTest(t)
	newTestChannel(t, "channel-a", 10, "model-1")
	newTestChannel(t, "channel-b", 20, "model-1", "model-2")

	route, err := ResolveRoute("model-1")
	require.NoError(t, err)
	assert.Equal(t, RouteSourceImplicit, route.Source)
	assert.Equal(t, []string{"channel-b", "channel-a"}, memberChannels(route))
	// 隐式成员的 upstream_model 就是路由键本身（未改名）。
	for _, m := range route.Members {
		assert.Equal(t, "model-1", m.UpstreamModel)
	}

	route2, err := ResolveRoute("model-2")
	require.NoError(t, err)
	assert.Equal(t, []string{"channel-b"}, memberChannels(route2))

	// 没有任何渠道声明该模型 → 空链（与"全挂"同形，由调用方返回 503）。
	route3, err := ResolveRoute("model-unknown")
	require.NoError(t, err)
	assert.Empty(t, route3.Members)
	assert.Equal(t, RouteSourceImplicit, route3.Source)
}

// 相同 priority 时按渠道 id 升序，保证顺序确定（不随查询计划抖动）。
func TestImplicitChainTieBreaksById(t *testing.T) {
	setupLaneTest(t)
	first := newTestChannel(t, "channel-first", 5, "model-1")
	newTestChannel(t, "channel-second", 5, "model-1")

	route, err := ResolveRoute("model-1")
	require.NoError(t, err)
	require.Len(t, route.Members, 2)
	assert.Equal(t, first.Id, route.Members[0].ChannelId)
}

// 显式车道同名时覆盖隐式链，并支持成员改名与成员级覆盖。
func TestExplicitLaneOverridesImplicitChain(t *testing.T) {
	setupLaneTest(t)
	channelA := newTestChannel(t, "channel-a", 10, "model-1")
	newTestChannel(t, "channel-b", 20, "model-1")

	require.NoError(t, UpsertLane(&Lane{
		Name:    "model-1",
		Enabled: true,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{
			ChannelId:     channelA.Id,
			UpstreamModel: "vendor-real-name",
			Priority:      1,
			Weight:        3,
			Overrides:     `{"member_max_attempts":1}`,
		}},
	}))

	route, err := ResolveRoute("model-1")
	require.NoError(t, err)
	assert.Equal(t, RouteSourceExplicit, route.Source)
	require.Len(t, route.Members, 1)
	assert.Equal(t, "channel-a", route.Members[0].Channel)
	assert.Equal(t, "vendor-real-name", route.Members[0].UpstreamModel)
	assert.Equal(t, 1, route.Members[0].Priority)
}

// 停用显式车道 → 回落隐式链（显式层只是覆盖层，关掉即回到默认语义）。
func TestDisabledExplicitLaneFallsBackToImplicit(t *testing.T) {
	setupLaneTest(t)
	newTestChannel(t, "channel-a", 10, "model-1")
	require.NoError(t, UpsertLane(&Lane{Name: "model-1", Enabled: false, Mode: LaneModeFailover}))

	route, err := ResolveRoute("model-1")
	require.NoError(t, err)
	assert.Equal(t, RouteSourceImplicit, route.Source)
	assert.Equal(t, []string{"channel-a"}, memberChannels(route))
}

// 成员别名点名：解析到所属显式车道，并记录被点名成员。
func TestResolveByPublicAliasPinsMember(t *testing.T) {
	setupLaneTest(t)
	channelA := newTestChannel(t, "channel-a", 10)
	channelB := newTestChannel(t, "channel-b", 5)

	require.NoError(t, UpsertLane(&Lane{
		Name:    "lane-alpha",
		Enabled: true,
		Mode:    LaneModeFailover,
		Members: []LaneMember{
			{ChannelId: channelA.Id, UpstreamModel: "model-x", PublicAlias: "fast", Priority: 1},
			{ChannelId: channelB.Id, UpstreamModel: "model-x", PublicAlias: "cheap", Priority: 2},
		},
	}))

	route, err := ResolveRoute("fast")
	require.NoError(t, err)
	assert.Equal(t, RouteSourceExplicit, route.Source)
	require.Len(t, route.Members, 2)
	assert.NotZero(t, route.PinnedMemberId)
	for _, m := range route.Members {
		if m.PublicAlias == "fast" {
			assert.Equal(t, route.PinnedMemberId, m.MemberId)
		}
	}
}

// 显式车道六键：未写的走默认，写了 0 的按默认补齐（Normalize 语义）。
func TestLaneConfigDefaultsAndOverrides(t *testing.T) {
	setupLaneTest(t)

	assert.Equal(t, LaneRelayConfig{
		MemberMaxAttempts:                     2,
		MemberRetryIntervalSeconds:            3,
		MemberNonStreamResponseTimeoutSeconds: 120,
		MemberStreamFirstEventTimeoutSeconds:  30,
		MemberCooldownSeconds:                 60,
		// 亲和默认 0：不做粘滞（design-v1 §7.3）。
		MemberAffinitySeconds: 0,
	}, ParseLaneRelayConfig(""))

	memberMax := 1
	merged := DefaultLaneRelayConfig().Apply(LaneRelayOverrides{MemberMaxAttempts: &memberMax})
	assert.Equal(t, 1, merged.MemberMaxAttempts)
	assert.Equal(t, 3, merged.MemberRetryIntervalSeconds)
}

// 思考后缀归一化：原文未命中时按 new-api 既有规则再匹配一次，路由键仍保留原文。
func TestResolveFallsBackToNormalizedModelName(t *testing.T) {
	setupLaneTest(t)
	const requested = "gpt-4-gizmo-abcd"
	normalized := ratio_setting.RoutingMatchModelName(requested)
	if normalized == "" || normalized == requested {
		t.Skipf("当前配置下 %q 不会被归一化，跳过", requested)
	}
	newTestChannel(t, "channel-a", 10, normalized)

	route, err := ResolveRoute(requested)
	require.NoError(t, err)
	require.Len(t, route.Members, 1)
	assert.Equal(t, requested, route.Model)
	assert.Equal(t, requested, route.Members[0].UpstreamModel)
}

// 可路由模型清单同时包含隐式（渠道声明）与显式（车道）两类。
func TestListModelSummaries(t *testing.T) {
	setupLaneTest(t)
	channelA := newTestChannel(t, "channel-a", 10, "model-1", "model-2")
	newTestChannel(t, "channel-b", 5, "model-1")
	require.NoError(t, UpsertLane(&Lane{
		Name:    "lane-pool",
		Enabled: true,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{ChannelId: channelA.Id, UpstreamModel: "vendor-x"}},
	}))

	summaries, err := ListModelSummaries()
	require.NoError(t, err)
	byName := map[string]ModelSummary{}
	for _, s := range summaries {
		byName[s.Model] = s
	}
	assert.Equal(t, RouteSourceImplicit, byName["model-1"].Source)
	assert.Equal(t, 2, byName["model-1"].MemberCount)
	assert.Equal(t, RouteSourceImplicit, byName["model-2"].Source)
	assert.Equal(t, RouteSourceExplicit, byName["lane-pool"].Source)
}
