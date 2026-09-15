package route

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"pbr/model"
	"pbr/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 选路与容错验收：尝试循环、错误分类、冷却、熔断三态、亲和、四种模式。
// 依据 docs/routing-spec-v1.md §2 / §3 / §4 / §5 / §6。

// testRoute 造一条独立车道的解析结果（车道名唯一，避免共享进程内运行态）。
func testRoute(lane string, members int, maxAttempts int) *model.ResolvedRoute {
	cfg := model.DefaultLaneRelayConfig()
	cfg.MemberMaxAttempts = maxAttempts
	cfg.MemberRetryIntervalSeconds = 0
	resolved := &model.ResolvedRoute{
		Model:  lane,
		Source: model.RouteSourceImplicit,
		Mode:   model.LaneModeFailover,
		Config: cfg,
	}
	for i := 0; i < members; i++ {
		resolved.Members = append(resolved.Members, model.RouteMember{
			ChannelId:     i + 1,
			Channel:       "channel-" + string(rune('a'+i)),
			UpstreamModel: "model-1",
			Priority:      100 - i,
			Weight:        1,
		})
	}
	return resolved
}

func apiError(status int, code types.ErrorCode, skipRetry bool) *types.NewAPIError {
	opts := []types.NewAPIErrorOptions{types.ErrOptionWithStatusCode(status)}
	if skipRetry {
		opts = append(opts, types.ErrOptionWithSkipRetry())
	}
	return types.NewError(errors.New("boom"), code, opts...)
}

func memberName(m *model.RouteMember) string {
	if m == nil {
		return ""
	}
	return m.Channel
}

// fakeClock 控制运行态的时间推进（冷却/熔断都依赖 Unix 毫秒）。
type fakeClock struct{ ms int64 }

func withFakeClock(t *testing.T) *fakeClock {
	t.Helper()
	original := nowMs
	clock := &fakeClock{ms: 1_700_000_000_000}
	nowMs = func() int64 { return clock.ms }
	t.Cleanup(func() { nowMs = original })
	return clock
}

func (c *fakeClock) advance(d time.Duration) { c.ms += d.Milliseconds() }

// ---------- 错误分类 ----------

func TestClassifyByStatusAndCode(t *testing.T) {
	cases := []struct {
		name string
		err  *types.NewAPIError
		want ErrorKind
	}{
		{"429 限流是软故障，不得按硬故障计", apiError(http.StatusTooManyRequests, "", false), KindSoftRateLimit},
		{"5xx 是软故障", apiError(http.StatusInternalServerError, "", false), KindSoftTransient},
		{"408 是软故障", apiError(http.StatusRequestTimeout, "", false), KindSoftTransient},
		{"401 是硬鉴权故障", apiError(http.StatusUnauthorized, "", false), KindHardAuth},
		{"403 是硬鉴权故障", apiError(http.StatusForbidden, "", false), KindHardAuth},
		{"普通 400 是客户端错误", apiError(http.StatusBadRequest, "", false), KindClientError},
		{"显式 skip-retry 的错误按客户端错误处理", apiError(http.StatusBadRequest, "", true), KindClientError},
		{"响应体不可解析属坏响应", apiError(0, types.ErrorCodeBadResponseBody, false), KindBadResponse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Classify(tc.err))
		})
	}
	assert.Equal(t, ErrorKind(""), Classify(nil))
}

// 欠费关键词优先于状态码：欠费类上游以 400 到达（design-v1 §7.6）。
func TestQuotaKeywordBeatsStatusCode(t *testing.T) {
	WithQuotaKeywordMatcher(t, func(message string) bool {
		return contains(message, "insufficient balance")
	})
	quotaErr := types.NewError(errors.New("400 insufficient balance"), "",
		types.ErrOptionWithStatusCode(http.StatusBadRequest))
	assert.Equal(t, KindHardQuota, Classify(quotaErr))
	assert.True(t, ShouldSwitchMember(KindHardQuota))
	assert.False(t, ShouldRetrySameMember(KindHardQuota))
}

func TestShouldSwitchMemberMatrix(t *testing.T) {
	assert.True(t, ShouldSwitchMember(KindSoftRateLimit))
	assert.True(t, ShouldSwitchMember(KindSoftTransient))
	assert.True(t, ShouldSwitchMember(KindHardAuth))
	assert.True(t, ShouldSwitchMember(KindHardQuota))
	assert.True(t, ShouldSwitchMember(KindBadResponse))
	assert.False(t, ShouldSwitchMember(KindClientError))
	assert.False(t, ShouldSwitchMember(KindCanceled))

	assert.True(t, ShouldRetrySameMember(KindSoftRateLimit))
	assert.True(t, ShouldRetrySameMember(KindSoftTransient))
	assert.False(t, ShouldRetrySameMember(KindHardAuth))
	assert.False(t, ShouldSwitchMember(ErrorKind("")))
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 || indexOfSubstring(haystack, needle) >= 0)
}

func indexOfSubstring(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func WithQuotaKeywordMatcher(t *testing.T, matcher func(string) bool) {
	t.Helper()
	previous := quotaKeywordMatcher
	SetQuotaKeywordMatcher(matcher)
	t.Cleanup(func() { quotaKeywordMatcher = previous })
}

// ---------- 尝试循环 ----------

// 软故障先用满单成员预算，再换下一个成员（routing-spec §3.1）。
func TestSoftFailureRetriesSameMemberThenFailsOver(t *testing.T) {
	withFakeClock(t)
	state := NewState(testRoute("lane-retry-budget", 2, 2))

	member, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-a", memberName(member))

	soft := apiError(http.StatusInternalServerError, "", false)

	member, _, ok = state.Next(soft)
	require.True(t, ok)
	assert.Equal(t, "channel-a", memberName(member), "预算未用完应原地重试同一成员")

	member, _, ok = state.Next(soft)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "预算用尽应换下一个成员")

	member, _, ok = state.Next(soft)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member))

	_, _, ok = state.Next(soft)
	assert.False(t, ok, "全部成员耗尽")
}

// 硬故障不在原地重试，立即换人。
func TestHardAuthFailsOverImmediately(t *testing.T) {
	withFakeClock(t)
	state := NewState(testRoute("lane-hard-auth", 2, 2))
	_, _, ok := state.Next(nil)
	require.True(t, ok)

	member, _, ok := state.Next(apiError(http.StatusUnauthorized, "", false))
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member))
}

// client_error 不换人、不冷却、不记熔断（否则一个坏请求会把健康成员打冷）。
func TestClientErrorDoesNotFailOverNorCooldown(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-client-error", 2, 2)
	state := NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)

	_, _, ok = state.Next(apiError(http.StatusBadRequest, "", true))
	assert.False(t, ok)

	runtime := Default.For(resolved.Model)
	assert.Empty(t, runtime.Cooldowns, "client_error 不得触发冷却")
	assert.Empty(t, runtime.Circuits, "client_error 不得记入熔断计分")
}

func TestSingleMemberExhaustion(t *testing.T) {
	withFakeClock(t)
	state := NewState(testRoute("lane-single", 1, 1))
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	_, _, ok = state.Next(apiError(http.StatusInternalServerError, "", false))
	assert.False(t, ok)
	assert.Equal(t, 1, state.MaxRetriesForLoop())
}

func TestMaxRetriesForLoopCoversAllMembers(t *testing.T) {
	assert.Equal(t, 4, NewState(testRoute("lane-max-2x2", 2, 2)).MaxRetriesForLoop())
	assert.Equal(t, 3, NewState(testRoute("lane-max-3x1", 3, 1)).MaxRetriesForLoop())
}

// 同成员原地重试要按 member_retry_interval_seconds 给出等待时长（§3.1）。
func TestRetryIntervalReportedForSameMemberRetry(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-retry-interval", 1, 3)
	resolved.Config.MemberRetryIntervalSeconds = 7
	state := NewState(resolved)

	_, delay, ok := state.Next(nil)
	require.True(t, ok)
	assert.Zero(t, delay)

	_, delay, ok = state.Next(apiError(http.StatusInternalServerError, "", false))
	require.True(t, ok)
	assert.Equal(t, 7*time.Second, delay)
}

func TestPinnedMemberTriedFirst(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-pinned", 3, 1)
	resolved.PinnedMemberId = 42
	resolved.Members[2].MemberId = 42
	state := NewState(resolved)

	member, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-c", memberName(member))

	member, _, ok = state.Next(apiError(http.StatusInternalServerError, "", false))
	require.True(t, ok)
	assert.Equal(t, "channel-a", memberName(member))
}

func TestNoMembersImmediatelyExhausted(t *testing.T) {
	withFakeClock(t)
	state := NewState(testRoute("lane-empty", 0, 2))
	_, _, ok := state.Next(nil)
	assert.False(t, ok)
}

// ---------- 冷却与熔断 ----------

// 成员预算耗尽 → 冷却期间被跳过；冷却到期 → 只放行一个探测（§6）。
func TestCooldownSkipsThenAllowsSingleProbe(t *testing.T) {
	clock := withFakeClock(t)
	resolved := testRoute("lane-cooldown", 2, 1)
	resolved.Config.MemberCooldownSeconds = 5
	state := NewState(resolved)

	member, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-a", memberName(member))
	soft := apiError(http.StatusInternalServerError, "", false)
	member, _, ok = state.Next(soft)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "channel-a 冷却后应换人")

	// 冷却未到期：新请求直接落到 channel-b。
	next := NewState(resolved)
	member, _, ok = next.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member))

	// 冷却到期：channel-a 重新进入候选，并且只放行一个探测。
	clock.advance(6 * time.Second)
	next = NewState(resolved)
	member, _, ok = next.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-a", memberName(member), "到期后应重新尝试高优先级成员")
	runtime := Default.For(resolved.Model)
	runtime.withLock(func() {
		assert.True(t, runtime.HasProbe, "探测槽应被占用")
	})

	// 探测槽未释放前，第二个并发请求不得再打同一成员。
	other := NewState(resolved)
	member, _, ok = other.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "探测槽单槽：其他请求应落到下一成员")
}

// 熔断：连续软故障达阈值 → 打开并快抛；退避期到 → 半开放一个探测；成功 → 复通。
func TestCircuitOpensHalfOpensAndRecovers(t *testing.T) {
	clock := withFakeClock(t)
	resolved := testRoute("lane-circuit", 1, 1)
	resolved.Config.MemberCooldownSeconds = 1
	soft := apiError(http.StatusInternalServerError, "", false)
	runtime := Default.For(resolved.Model)

	failOnce := func(step string) {
		state := NewState(resolved)
		_, _, ok := state.Next(nil)
		if !ok {
			t.Fatalf("%s: 期望拿到成员", step)
		}
		_, _, ok = state.Next(soft)
		assert.False(t, ok, "%s: 唯一成员失败后应无可用", step)
	}

	// 阈值 2、5xx 权重 0.6 → 第 4 次失败时打开。
	failOnce("第 1 次")
	clock.advance(1100 * time.Millisecond)
	failOnce("第 2 次")
	clock.advance(1100 * time.Millisecond)
	failOnce("第 3 次")
	clock.advance(1100 * time.Millisecond)
	failOnce("第 4 次")

	runtime.withLock(func() {
		circuit := runtime.Circuits[memberKeyOf(&resolved.Members[0])]
		require.NotNil(t, circuit)
		assert.Equal(t, CircuitOpen, circuit.State, "累计失败应打开熔断")
	})
	assert.Contains(t, eventTypes(runtime), EventCircuitOpen)

	// 打开期间：即便冷却已过，也不得放行请求（错误快抛，不轮询等待）。
	clock.advance(1500 * time.Millisecond)
	opened := NewState(resolved)
	_, _, ok := opened.Next(nil)
	require.True(t, ok, "退避到期应放行一个半开探测")
	runtime.withLock(func() {
		circuit := runtime.Circuits[memberKeyOf(&resolved.Members[0])]
		assert.Equal(t, CircuitHalfOpen, circuit.State, "退避到期应进入半开")
	})
	assert.Contains(t, eventTypes(runtime), EventCircuitHalfOpen)

	// 上游已修好：探测成功 → 复通（closed），并写恢复事件。
	opened.OnSuccess()
	runtime.withLock(func() {
		circuit := runtime.Circuits[memberKeyOf(&resolved.Members[0])]
		assert.Equal(t, CircuitClosed, circuit.State, "半开探测成功应复通")
		assert.Zero(t, circuit.Score)
		assert.Empty(t, runtime.Cooldowns)
	})
	assert.Contains(t, eventTypes(runtime), EventCircuitClosed)

	// 复通后立即可正常服务。
	recovered := NewState(resolved)
	_, _, ok = recovered.Next(nil)
	assert.True(t, ok)
}

// 429 权重低：持续限流不应像硬故障那样快速打开熔断（design-v1 §7.5）。
func TestRateLimitDoesNotOpenCircuitAsFastAsHardFailure(t *testing.T) {
	clock := withFakeClock(t)
	softRoute := testRoute("lane-soft-weight", 1, 1)
	softRoute.Config.MemberCooldownSeconds = 1
	soft := apiError(http.StatusTooManyRequests, "", false)

	for i := 0; i < 4; i++ {
		state := NewState(softRoute)
		_, _, ok := state.Next(nil)
		require.True(t, ok)
		state.Next(soft)
		clock.advance(1100 * time.Millisecond)
	}
	runtime := Default.For(softRoute.Model)
	runtime.withLock(func() {
		circuit := runtime.Circuits[memberKeyOf(&softRoute.Members[0])]
		require.NotNil(t, circuit)
		assert.NotEqual(t, CircuitOpen, circuit.State, "4 次 429（权重 0.2）不应打开熔断")
	})
}

// 指数退避：重复打开时时长按 2^k 增长（设上限）。
func TestCircuitBackoffGrowsOnRepeatedOpen(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-backoff", 1, 1)
	resolved.Config.MemberCooldownSeconds = 1
	runtime := Default.For(resolved.Model)
	settings := DefaultCircuitSettings()
	settings.OpenSeconds = 1
	settings.MaxOpenSeconds = 8

	key := memberKeyOf(&resolved.Members[0])
	var durations []int64
	for i := 0; i < 3; i++ {
		runtime.withLock(func() {
			circuit := runtime.Circuits[key]
			if circuit == nil {
				circuit = &Circuit{State: CircuitClosed}
				runtime.Circuits[key] = circuit
			}
			runtime.openCircuit(key, circuit, nowMs(), 1, settings)
			durations = append(durations, (circuit.OpenUntil-nowMs())/1000)
		})
	}
	assert.Equal(t, []int64{1, 2, 4}, durations)
}

// ---------- 亲和 ----------

// 亲和默认 0：不做粘滞，高优先级成员恢复后立刻切回（design-v1 §7.3）。
func TestDefaultAffinityDoesNotStick(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-affinity-default", 2, 1)
	assert.Zero(t, resolved.Config.MemberAffinitySeconds)

	state := NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	// 高优先级成员失败 → 切到低优先级成员并成功。
	member, _, ok := state.Next(apiError(http.StatusUnauthorized, "", false))
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member))
	state.OnSuccess()

	// 亲和为 0 → 下一个请求应重新回到高优先级成员（其冷却已过）。
	runtime := Default.For(resolved.Model)
	runtime.withLock(func() { runtime.Cooldowns = map[string]int64{} })
	next := NewState(resolved)
	member, _, ok = next.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-a", memberName(member))
}

// 显式配置亲和：切到低优先级成员成功后，亲和期内保持粘滞（routing-spec §6）。
func TestConfiguredAffinitySticksWithinWindow(t *testing.T) {
	clock := withFakeClock(t)
	resolved := testRoute("lane-affinity-set", 2, 1)
	resolved.Config.MemberAffinitySeconds = 300

	state := NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	member, _, ok := state.Next(apiError(http.StatusUnauthorized, "", false))
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member))
	state.OnSuccess()

	runtime := Default.For(resolved.Model)
	runtime.withLock(func() {
		assert.True(t, runtime.AffinityUntil > nowMs(), "成功后应启动亲和")
		runtime.Cooldowns = map[string]int64{}
	})
	clock.advance(10 * time.Second)
	next := NewState(resolved)
	member, _, ok = next.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "亲和期内应保持当前成员")

	clock.advance(400 * time.Second)
	after := NewState(resolved)
	member, _, ok = after.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-a", memberName(member), "亲和过期后应回到高优先级成员")
}

// ---------- 四种模式 ----------

func TestManualModeUsesOnlyActiveMember(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-manual", 3, 1)
	resolved.Mode = model.LaneModeManual
	resolved.ActiveMember = "channel-b/model-1"
	state := NewState(resolved)

	member, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member))

	// manual 语义是"就要这一个"：失败后直接无可用，不静默换人（§2.1）。
	_, _, ok = state.Next(apiError(http.StatusInternalServerError, "", false))
	assert.False(t, ok)
}

func TestManualModeWithoutActiveMemberIsUnavailable(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-manual-empty", 2, 1)
	resolved.Mode = model.LaneModeManual
	state := NewState(resolved)
	_, _, ok := state.Next(nil)
	assert.False(t, ok)
}

func TestRoundRobinModeCyclesMembers(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-round-robin", 3, 1)
	resolved.Mode = model.LaneModeRoundRobin

	var order []string
	for i := 0; i < 4; i++ {
		state := NewState(resolved)
		member, _, ok := state.Next(nil)
		require.True(t, ok)
		order = append(order, memberName(member))
		state.OnSuccess()
	}
	assert.Equal(t, []string{"channel-a", "channel-b", "channel-c", "channel-a"}, order)
}

func TestWeightedModeRespectsZeroWeight(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-weighted-zero", 2, 1)
	resolved.Mode = model.LaneModeWeighted
	resolved.Members[0].Weight = 1
	resolved.Members[1].Weight = 0

	for i := 0; i < 10; i++ {
		state := NewState(resolved)
		member, _, ok := state.Next(nil)
		require.True(t, ok)
		assert.Equal(t, "channel-a", memberName(member), "权重为 0 的成员不应被选中")
	}
}

func TestWeightedModeSpreadsAcrossMembers(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-weighted-spread", 2, 1)
	resolved.Mode = model.LaneModeWeighted

	seen := map[string]int{}
	for i := 0; i < 60; i++ {
		state := NewState(resolved)
		member, _, ok := state.Next(nil)
		require.True(t, ok)
		seen[memberName(member)]++
	}
	assert.NotZero(t, seen["channel-a"])
	assert.NotZero(t, seen["channel-b"])
}

func TestNoAvailableMessageIsStable(t *testing.T) {
	// 下游 fallback 分类依赖这个固定文案（routing-spec §4.2）。
	assert.Equal(t, "No available channel for model model-1", NoAvailableMessage("model-1"))
	err := NoAvailableError("model-1")
	require.NotNil(t, err)
	assert.True(t, IsNoAvailable(err))
	assert.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
}

func eventTypes(runtime *Runtime) []string {
	var out []string
	runtime.withLock(func() {
		for _, event := range runtime.Events {
			out = append(out, event.Type)
		}
	})
	return out
}

// ---------- 源码审计修复的回归用例 ----------

// 探测槽：weighted 模式下"冷却到期但没被选中"的成员不得占住探测槽。
func TestWeightedDoesNotHoldProbeWhenAnotherMemberChosen(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-probe-hold", 2, 1)
	resolved.Mode = model.LaneModeWeighted
	// 成员 a 权重 0（不会随机被选中），且冷却已到期（可探测）；成员 b 正常
	resolved.Members[0].Weight = 0
	resolved.Members[1].Weight = 1
	runtime := Default.For(resolved.Model)
	runtime.withLock(func() {
		runtime.Cooldowns[memberKeyOf(&resolved.Members[0])] = nowMs() - 1
	})

	state := NewState(resolved)
	member, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "权重 0 的探测成员不应被选中")
	runtime.withLock(func() {
		assert.False(t, runtime.HasProbe, "没选中它就不该占探测槽（否则槽永久泄漏）")
	})

	// 把 b 的预算用掉后，只剩 a 可探测 → 必须还能拿到探测位，证明槽没被泄漏
	next := NewState(resolved)
	next.attempts[1] = next.budgetFor(1)
	member, _, ok = next.Next(nil)
	require.True(t, ok, "槽未被占用时应能放行探测")
	assert.Equal(t, "channel-a", memberName(member))
	runtime.withLock(func() {
		assert.True(t, runtime.HasProbe)
	})
}

// 探测槽：client_error / canceled 不换人，收尾必须能归还（否则该成员被永久跳过）。
func TestReleaseProbeReturnsSlot(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-probe-release", 1, 1)
	runtime := Default.For(resolved.Model)

	state := NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	runtime.withLock(func() {
		runtime.Cooldowns[memberKeyOf(&resolved.Members[0])] = nowMs() - 1 // 冷却到期 → 下次走探测
	})
	state.ReleaseProbe()

	next := NewState(resolved)
	member, _, ok := next.Next(nil)
	require.True(t, ok, "归还后应能再次放行探测")
	assert.Equal(t, "channel-a", memberName(member))
	runtime.withLock(func() {
		assert.True(t, runtime.HasProbe, "这次确实占用了探测槽")
	})
}

// 运行态键：别名点名与车道名必须共享同一份运行态（routing-spec §1.3「每车道一份」）。
func TestAliasAndLaneShareRuntime(t *testing.T) {
	withFakeClock(t)
	laneRoute := testRoute("lane-shared", 1, 1)
	aliasRoute := testRoute("lane-shared", 1, 1)
	aliasRoute.Model = "some-alias" // 别名点名：Model 是别名，RouteKey 仍是车道名

	assert.Equal(t, Default.For(laneRoute.Model), NewState(laneRoute).Runtime)
	assert.Equal(t, Default.For(laneRoute.Model), NewState(aliasRoute).Runtime,
		"别名点名必须落到同一份车道运行态")
}

// 成员级覆盖：预算 / 冷却 / 重试间隔都要按成员生效（design-v1 §7.3、routing-spec §3.1）。
func TestMemberLevelOverridesTakeEffect(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-member-overrides", 2, 3)
	resolved.Members[0].Overrides = `{"member_max_attempts":1,"member_retry_interval_seconds":0}`
	resolved.Members[1].Overrides = `{"member_max_attempts":2}`

	state := NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	// 成员 a 覆盖为 1 次预算：一次失败即换人
	member, _, ok := state.Next(apiError(http.StatusInternalServerError, "", false))
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "成员级 max_attempts=1 应只试一次就换人")

	// 上界 = 各成员预算之和 = 1 + 2
	assert.Equal(t, 3, state.MaxRetriesForLoop())
}

// 成员级冷却覆盖：冷却时长按成员值写入运行态。
func TestMemberLevelCooldownOverride(t *testing.T) {
	clock := withFakeClock(t)
	resolved := testRoute("lane-member-cooldown", 1, 1)
	resolved.Config.MemberCooldownSeconds = 60
	resolved.Members[0].Overrides = `{"member_cooldown_seconds":3}`

	state := NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	state.Next(apiError(http.StatusInternalServerError, "", false))

	runtime := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])
	runtime.withLock(func() {
		delta := runtime.Cooldowns[key] - clock.ms
		assert.LessOrEqual(t, delta, int64(3000), "应使用成员级 3s 冷却而不是车道级 60s")
	})
}

// Classify：基座的 NewError 默认状态码是 500，我方内部错误都是"500 + skipRetry"，
// 所以 skipRetry 必须优先，否则坏请求会把健康成员打冷（§4.1 禁止）。
func TestClassifySkipRetryWinsOverDefaultStatus(t *testing.T) {
	internalErr := types.NewError(errors.New("convert request failed"), types.ErrorCodeConvertRequestFailed,
		types.ErrOptionWithSkipRetry())
	assert.Equal(t, http.StatusInternalServerError, internalErr.StatusCode, "基座默认状态码就是 500")
	assert.Equal(t, KindClientError, Classify(internalErr), "带 skipRetry 的我方错误不得触发换人/冷却")

	// 上游错误不带 skipRetry，按状态码判定
	upstream5xx := types.NewError(errors.New("upstream boom"), types.ErrorCodeDoRequestFailed,
		types.ErrOptionWithStatusCode(http.StatusInternalServerError))
	assert.Equal(t, KindSoftTransient, Classify(upstream5xx))
}

// Classify：成员耗尽产生的 503 不能记成 client_error（routing-spec §4.2/§9）。
func TestNoAvailableIsNotClientError(t *testing.T) {
	kind := Classify(NoAvailableError("model-x"))
	assert.NotEqual(t, KindClientError, kind)
	assert.Equal(t, KindSoftTransient, kind)
}

// Classify：客户端取消仍按 canceled 处理（不换人、不冷却）。
func TestCanceledStaysCanceled(t *testing.T) {
	canceled := types.NewError(fmt.Errorf("read body: %w", context.Canceled), types.ErrorCodeBadResponseBody)
	assert.Equal(t, KindCanceled, Classify(canceled))
}

// ---------- attempts[].duration_ms（routing-spec §9） ----------

// 每次失败尝试都必须带非零耗时。此前 DurationMs 声明了却从不赋值，
// 加上 omitempty 后该字段在响应里根本不出现，排障时无法判断"哪个成员慢"。
func TestFailedAttemptRecordsDuration(t *testing.T) {
	withFakeClock(t)
	state := NewState(testRoute("lane-duration", 1, 1))

	_, _, ok := state.Next(nil)
	require.True(t, ok)

	// 模拟一次真实尝试耗时 250ms 后失败
	state.attemptStartedAt = time.Now().Add(-250 * time.Millisecond)
	_, _, ok = state.Next(apiError(http.StatusInternalServerError, "", false))
	require.False(t, ok, "单成员预算耗尽")

	history := state.History()
	require.Len(t, history, 1)
	assert.Equal(t, "failed", history[0].Status)
	assert.GreaterOrEqual(t, history[0].DurationMs, int64(200),
		"失败尝试必须记录真实耗时（此前恒为 0 且因 omitempty 不出现）")
}

// 成功尝试同样带耗时（api-spec §4.4 示例：success 项含 duration_ms）。
func TestSuccessAttemptRecordsDuration(t *testing.T) {
	withFakeClock(t)
	state := NewState(testRoute("lane-duration-ok", 1, 1))

	_, _, ok := state.Next(nil)
	require.True(t, ok)
	state.attemptStartedAt = time.Now().Add(-120 * time.Millisecond)

	attempt := state.SuccessAttempt("channel-a/model-1")
	assert.Equal(t, "success", attempt.Status)
	assert.GreaterOrEqual(t, attempt.DurationMs, int64(100),
		"成功尝试必须记录耗时")
}

// client_error 不换人，但仍是一次真实尝试，必须留痕并带耗时。
func TestClientErrorAttemptStillRecorded(t *testing.T) {
	withFakeClock(t)
	state := NewState(testRoute("lane-duration-client", 1, 1))

	_, _, ok := state.Next(nil)
	require.True(t, ok)
	state.attemptStartedAt = time.Now().Add(-80 * time.Millisecond)

	// 400 → client_error：不换人、不冷却
	_, _, ok = state.Next(apiError(http.StatusBadRequest, "", true))
	require.False(t, ok)

	history := state.History()
	require.Len(t, history, 1, "client_error 也应留痕（否则最后一次尝试从日志里消失）")
	assert.Equal(t, "failed", history[0].Status)
	assert.GreaterOrEqual(t, history[0].DurationMs, int64(50))
}

// ---------- 被跳过成员的留痕（routing-spec §9） ----------

// 冷却中的高优先级成员必须留下 cooldown 记录，用于解释"为什么没用 P1"。
func TestCooldownMemberRecordedAsCooldown(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-skip-cooldown", 2, 1)
	state := NewState(resolved)

	// 让 ch1 进入冷却（未到期）
	state.Runtime.withLock(func() {
		state.Runtime.Cooldowns[memberKeyOf(&resolved.Members[0])] = nowMs() + 60_000
	})

	member, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "冷却中的 ch1 应被跳过")

	statuses := map[string]string{}
	for _, a := range state.History() {
		statuses[a.Member] = a.Status
	}
	assert.Equal(t, "cooldown", statuses["channel-a/model-1"],
		"被冷却跳过的成员必须留下 cooldown 记录")
}

// 熔断打开的成员留下 circuit_break 记录。
func TestCircuitOpenMemberRecordedAsCircuitBreak(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-skip-circuit", 2, 1)
	state := NewState(resolved)

	state.Runtime.withLock(func() {
		key := memberKeyOf(&resolved.Members[0])
		state.Runtime.Circuits[key] = &Circuit{
			State:     CircuitOpen,
			OpenUntil: nowMs() + 60_000,
		}
	})

	member, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member))

	statuses := map[string]string{}
	for _, a := range state.History() {
		statuses[a.Member] = a.Status
	}
	assert.Equal(t, "circuit_break", statuses["channel-a/model-1"],
		"熔断打开的成员必须留下 circuit_break 记录")
}

// 真实尝试过的成员进冷却后，不应再补一条 cooldown：它为什么没被继续用，
// 前一条 failed 已经解释清楚。重复记录会把尝试链写成
// [failed(A), cooldown(A)]——同一成员在同一请求里既"失败过"又"被跳过"，
// 自相矛盾且误导排障（routing-spec §9 的 status 每成员每请求只应有一种含义）。
func TestFailedMemberNotAlsoRecordedAsCooldown(t *testing.T) {
	withFakeClock(t)
	// 预算 2：ch1 首发硬故障后**预算仍有剩余**，于是本轮选路会再次遍历到它；
	// 而它此刻已因失败进冷却——这正是重复记录的触发条件。
	state := NewState(testRoute("lane-skip-after-fail", 2, 2))

	_, _, ok := state.Next(nil)
	require.True(t, ok)
	_, _, ok = state.Next(apiError(http.StatusUnauthorized, "", false))
	require.True(t, ok, "硬故障应立刻换到 ch2")

	history := state.History()
	require.Len(t, history, 1, "只应有 ch1 那条真实失败尝试，不该再补一条 cooldown(ch1)")
	assert.Equal(t, "failed", history[0].Status)
	assert.Equal(t, "channel-a/model-1", history[0].Member)
	assert.Equal(t, KindHardAuth, history[0].ErrorKind)
}

// LogAttempts 是"快抛 503"路径落库用的转换（routing-spec §4.2：快抛前必须已把
// 本次所有尝试写入日志）。字段必须与 History 一一对应，否则中间件落库的尝试链
// 会与单测里看到的链不一致。
func TestLogAttemptsMirrorsHistory(t *testing.T) {
	withFakeClock(t)
	state := NewState(testRoute("lane-log-attempts", 2, 1))

	_, _, ok := state.Next(nil)
	require.True(t, ok)
	state.attemptStartedAt = time.Now().Add(-150 * time.Millisecond)
	_, _, ok = state.Next(apiError(http.StatusInternalServerError, "", false))
	require.True(t, ok)

	history := state.History()
	attempts := state.LogAttempts()
	require.Len(t, attempts, len(history))
	require.NotEmpty(t, attempts)
	for i, item := range attempts {
		assert.Equal(t, history[i].AttemptNum, item.AttemptNum)
		assert.Equal(t, history[i].Member, item.Member)
		assert.Equal(t, history[i].Status, item.Status)
		assert.Equal(t, history[i].DurationMs, item.DurationMs)
		assert.Equal(t, string(history[i].ErrorKind), item.ErrorKind)
		assert.Equal(t, history[i].Msg, item.Msg)
	}
	assert.Equal(t, "failed", attempts[0].Status)
	assert.GreaterOrEqual(t, attempts[0].DurationMs, int64(100))
}

// 同一成员多轮被跳过时只记一次，避免尝试链被噪声撑大。
func TestSkipRecordedOncePerMember(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-skip-dedup", 2, 2)
	state := NewState(resolved)

	state.Runtime.withLock(func() {
		state.Runtime.Cooldowns[memberKeyOf(&resolved.Members[0])] = nowMs() + 60_000
	})

	// ch2 预算为 2：前两次选中 ch2，第三次起 ch2 预算耗尽（ch1 仍冷却中）
	for i := 0; i < 3; i++ {
		_, _, _ = state.Next(nil)
	}
	count := 0
	for _, a := range state.History() {
		if a.Member == "channel-a/model-1" {
			count++
		}
	}
	assert.Equal(t, 1, count, "同一成员只应记一次跳过（多轮重试会重复跳过它）")
}
