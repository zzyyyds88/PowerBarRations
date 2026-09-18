package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 路由核心验收：模型名即路由键、车道是唯一入口（ADR 0005、docs/routing-spec-v1.md §1）。

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

func newTestChannel(t *testing.T, name string, models ...string) *Channel {
	t.Helper()
	channel := &Channel{
		Name:   name,
		Models: strings.Join(models, ","),
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Key:    "sk-test",
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

// 没有车道的模型不可调用：运行期空链（503），展示面给出"渠道声明"的建议链（ADR 0005）。
func TestLaneRequiredWithoutLane(t *testing.T) {
	setupLaneTest(t)
	newTestChannel(t, "channel-a", "model-1")
	newTestChannel(t, "channel-b", "model-1", "model-2")

	route, err := ResolveRoute("model-1")
	require.NoError(t, err)
	assert.Equal(t, RouteSourceUnconfigured, route.Source)
	assert.Empty(t, route.Members)

	// 展示面：候选链按渠道 id 升序（渠道 priority 已删除；仅供界面"添加成员"）。
	display, err := ResolveRouteForDisplay("model-1")
	require.NoError(t, err)
	assert.Equal(t, RouteSourceUnconfigured, display.Source)
	assert.Equal(t, []string{"channel-a", "channel-b"}, memberChannels(display))
	for _, m := range display.Members {
		assert.Equal(t, "model-1", m.UpstreamModel)
	}

	// 未声明的模型：建议链也是空。
	unknown, err := ResolveRouteForDisplay("model-unknown")
	require.NoError(t, err)
	assert.Empty(t, unknown.Members)
}

// 建议链相同 priority 时按渠道 id 升序，保证顺序确定（不随查询计划抖动）。
func TestSuggestedMembersTieBreaksById(t *testing.T) {
	setupLaneTest(t)
	first := newTestChannel(t, "channel-first", "model-1")
	newTestChannel(t, "channel-second", "model-1")

	display, err := ResolveRouteForDisplay("model-1")
	require.NoError(t, err)
	require.Len(t, display.Members, 2)
	assert.Equal(t, first.Id, display.Members[0].ChannelId)
}

// 车道成员支持改名与成员级覆盖。
func TestExplicitLaneOverridesMembers(t *testing.T) {
	setupLaneTest(t)
	channelA := newTestChannel(t, "channel-a", "model-1")
	newTestChannel(t, "channel-b", "model-1")

	require.NoError(t, UpsertLane(&Lane{
		Name:    "model-1",
		Enabled: true,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{
			ChannelId:     channelA.Id,
			UpstreamModel: "vendor-real-name",
			Priority:      1,
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

// 停用车道的模型不可调用（不再回落隐式链，ADR 0005）。
func TestDisabledLaneIsNotRoutable(t *testing.T) {
	setupLaneTest(t)
	newTestChannel(t, "channel-a", "model-1")
	require.NoError(t, UpsertLane(&Lane{Name: "model-1", Enabled: false, Mode: LaneModeFailover}))

	route, err := ResolveRoute("model-1")
	require.NoError(t, err)
	assert.Equal(t, RouteSourceUnconfigured, route.Source)
	assert.Empty(t, route.Members)
}

// 成员别名点名：解析到所属车道，并记录被点名成员。
func TestResolveByPublicAliasPinsMember(t *testing.T) {
	setupLaneTest(t)
	channelA := newTestChannel(t, "channel-a")
	channelB := newTestChannel(t, "channel-b")

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

// 渠道 model_mapping：成员 upstream 留空时用映射；成员级显式改名优先。
func TestChannelModelMappingResolvesUpstream(t *testing.T) {
	setupLaneTest(t)
	mapping, _ := json.Marshal(map[string]string{"model-1": "vendor-a/real-1"})
	mappingJSON := string(mapping)
	channel := &Channel{
		Name:         "channel-a",
		Models:       "model-1",
		Status:       common.ChannelStatusEnabled,
		Group:        "default",
		Key:          "sk-test",
		ModelMapping: &mappingJSON,
	}
	require.NoError(t, DB.Create(channel).Error)

	require.NoError(t, UpsertLane(&Lane{
		Name:    "model-1",
		Enabled: true,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{ChannelId: channel.Id, Priority: 10}},
	}))
	route, err := ResolveRoute("model-1")
	require.NoError(t, err)
	require.Len(t, route.Members, 1)
	assert.Equal(t, "vendor-a/real-1", route.Members[0].UpstreamModel)

	// 成员级显式改名覆盖渠道映射。
	require.NoError(t, UpsertLane(&Lane{
		Name:    "model-1",
		Enabled: true,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{ChannelId: channel.Id, UpstreamModel: "member-override", Priority: 10}},
	}))
	route, err = ResolveRoute("model-1")
	require.NoError(t, err)
	assert.Equal(t, "member-override", route.Members[0].UpstreamModel)
}

// SeedLanes：为渠道声明但无车道的模型生成 failover 车道；幂等。
func TestSeedLanesCreatesMissingLanes(t *testing.T) {
	setupLaneTest(t)
	channelA := newTestChannel(t, "channel-a", "model-1", "model-2")
	newTestChannel(t, "channel-b", "model-1")
	require.NoError(t, UpsertLane(&Lane{
		Name:    "model-1",
		Enabled: true,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{ChannelId: channelA.Id, Priority: 10}},
	}))

	created, skipped, err := SeedLanes(false)
	require.NoError(t, err)
	assert.Equal(t, []string{"model-2"}, created)
	assert.Equal(t, []string{"model-1"}, skipped)

	lane, err := GetLaneByName("model-2")
	require.NoError(t, err)
	require.Len(t, lane.Members, 1)
	assert.Equal(t, channelA.Id, lane.Members[0].ChannelId)
	assert.Empty(t, lane.Members[0].UpstreamModel)

	created2, _, err := SeedLanes(false)
	require.NoError(t, err)
	assert.Empty(t, created2)
}

// SeedLanes 的初始顺序必须与文档一致：按渠道 id 升序（routing-spec §1.1、
// design-v1 §7.7）。resolveExactRoute 按 priority 降序排序，若 seed 直接写
// Priority=c.Id，落库后实际会先打渠道 id 最大者——与文档相反。seed 必须让
// priority 随渠道 id 升序递减，使"排序后的实际顺序"仍是 id 升序。
func TestSeedLanesOrderFollowsChannelIdAscending(t *testing.T) {
	setupLaneTest(t)
	first := newTestChannel(t, "channel-first", "model-seed-order")
	second := newTestChannel(t, "channel-second", "model-seed-order")
	third := newTestChannel(t, "channel-third", "model-seed-order")
	require.Less(t, first.Id, second.Id)
	require.Less(t, second.Id, third.Id)

	created, _, err := SeedLanes(false)
	require.NoError(t, err)
	require.Contains(t, created, "model-seed-order")

	// 落库顺序（GetLaneByName 按 priority desc 读取）必须是 id 升序。
	lane, err := GetLaneByName("model-seed-order")
	require.NoError(t, err)
	require.Len(t, lane.Members, 3)
	assert.Equal(t, []int{first.Id, second.Id, third.Id}, laneMemberChannelIds(lane))
	assert.Greater(t, lane.Members[0].Priority, lane.Members[1].Priority,
		"priority 必须随渠道 id 升序递减（数字大者优先）")
	assert.Greater(t, lane.Members[1].Priority, lane.Members[2].Priority)

	// 运行期解析顺序（resolveExactRoute 按 priority 降序）同样必须是 id 升序。
	resolved, err := ResolveRoute("model-seed-order")
	require.NoError(t, err)
	require.Len(t, resolved.Members, 3)
	assert.Equal(t, []string{"channel-first", "channel-second", "channel-third"},
		memberChannels(resolved), "seed 后实际先打渠道 id 最小者")
}

func laneMemberChannelIds(lane *Lane) []int {
	out := make([]int, 0, len(lane.Members))
	for _, m := range lane.Members {
		out = append(out, m.ChannelId)
	}
	return out
}

func TestSeedLanesDryRun(t *testing.T) {
	setupLaneTest(t)
	newTestChannel(t, "channel-a", "model-1")

	created, _, err := SeedLanes(true)
	require.NoError(t, err)
	assert.Equal(t, []string{"model-1"}, created)
	if _, err := GetLaneByName("model-1"); err == nil {
		t.Fatal("dry run must not persist lanes")
	}
}

// 车道六键：未写的走默认，写了 0 的按默认补齐（Normalize 语义）。
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
	channelA := newTestChannel(t, "channel-a", normalized)
	require.NoError(t, UpsertLane(&Lane{
		Name:    normalized,
		Enabled: true,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{ChannelId: channelA.Id, Priority: 10}},
	}))

	route, err := ResolveRoute(requested)
	require.NoError(t, err)
	require.Len(t, route.Members, 1)
	assert.Equal(t, requested, route.Model)
	// 成员 upstream 留空 → 上游名回落到车道名（归一化后的模型名）。
	assert.Equal(t, normalized, route.Members[0].UpstreamModel)
}

// 路由键清单：已配车道 routable=true，渠道声明但未配车道 unconfigured/false。
func TestListModelSummaries(t *testing.T) {
	setupLaneTest(t)
	channelA := newTestChannel(t, "channel-a", "model-1", "model-2")
	newTestChannel(t, "channel-b", "model-1")
	require.NoError(t, UpsertLane(&Lane{
		Name:    "lane-pool",
		Enabled: true,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{ChannelId: channelA.Id, UpstreamModel: "vendor-x"}},
	}))
	require.NoError(t, UpsertLane(&Lane{
		Name:    "model-1",
		Enabled: true,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{ChannelId: channelA.Id, Priority: 10}},
	}))

	summaries, err := ListModelSummaries()
	require.NoError(t, err)
	byName := map[string]ModelSummary{}
	for _, s := range summaries {
		byName[s.Model] = s
	}
	assert.Equal(t, RouteSourceExplicit, byName["model-1"].Source)
	assert.True(t, byName["model-1"].Routable)
	assert.Equal(t, 1, byName["model-1"].MemberCount)
	assert.Equal(t, RouteSourceUnconfigured, byName["model-2"].Source)
	assert.False(t, byName["model-2"].Routable)
	assert.Equal(t, 1, byName["model-2"].MemberCount)
	assert.Equal(t, RouteSourceExplicit, byName["lane-pool"].Source)
	assert.True(t, byName["lane-pool"].Routable)
}

// 停用车道也要在 /models 里可见（source=disabled、不可调用），
// 否则"只有一条停用车道的模型"会从控制台彻底消失（P2-8）。
func TestListModelSummariesIncludesDisabledLane(t *testing.T) {
	setupLaneTest(t)
	ch := newTestChannel(t, "channel-disabled", "only-disabled")
	require.NoError(t, UpsertLane(&Lane{
		Name:    "only-disabled",
		Enabled: false,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{ChannelId: ch.Id, Priority: 1}},
	}))

	summaries, err := ListModelSummaries()
	require.NoError(t, err)
	byName := map[string]ModelSummary{}
	for _, s := range summaries {
		byName[s.Model] = s
	}
	s, ok := byName["only-disabled"]
	require.True(t, ok, "停用车道必须仍出现在 /models")
	assert.Equal(t, RouteSourceDisabled, s.Source)
	assert.False(t, s.Routable)
	assert.Equal(t, 1, s.MemberCount)
}

// 停用车道的详情要返回真实成员链（供编辑），而不是空链/建议链。
func TestResolveRouteForDisplayReturnsMembersForDisabledLane(t *testing.T) {
	setupLaneTest(t)
	ch := newTestChannel(t, "channel-disabled-2", "disabled-route")
	require.NoError(t, UpsertLane(&Lane{
		Name:    "disabled-route",
		Enabled: false,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{ChannelId: ch.Id, Priority: 3}},
	}))

	route, err := ResolveRouteForDisplay("disabled-route")
	require.NoError(t, err)
	assert.Equal(t, RouteSourceDisabled, route.Source)
	require.Len(t, route.Members, 1)
	assert.Equal(t, "channel-disabled-2", route.Members[0].Channel)
	assert.True(t, route.Members[0].ChannelEnabled)
}

// available_member_count 只计"渠道存在且启用"的成员（P3-1）。
func TestListModelSummariesCountsAvailableMembers(t *testing.T) {
	setupLaneTest(t)
	chOK := newTestChannel(t, "avail-ok", "avail-model")
	chOff := newTestChannel(t, "avail-off", "avail-model")
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", chOff.Id).Update("status", common.ChannelStatusManuallyDisabled).Error)
	require.NoError(t, UpsertLane(&Lane{
		Name:    "avail-model",
		Enabled: true,
		Mode:    LaneModeFailover,
		Members: []LaneMember{{ChannelId: chOK.Id, Priority: 2}, {ChannelId: chOff.Id, Priority: 1}, {ChannelId: 999999, Priority: 0}},
	}))

	summaries, err := ListModelSummaries()
	require.NoError(t, err)
	for _, s := range summaries {
		if s.Model != "avail-model" {
			continue
		}
		assert.Equal(t, 3, s.MemberCount, "总数含停用与悬空成员")
		assert.Equal(t, 1, s.AvailableMemberCount, "可用数只计启用且存在的渠道")
		return
	}
	t.Fatal("avail-model not found")
}
