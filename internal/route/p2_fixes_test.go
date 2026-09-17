package route

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 只读审查 P2 修复的回归用例。
// 依据 docs/routing-spec-v1.md §4.1/§6/§9 与 docs/design-v1.md §7.5。

// --- P2-1：4xx 分类收敛，只有 400/422 是 client_error ---

func TestClassifyNarrowClientErrorTo400And422(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   ErrorKind
	}{
		{"400 是客户端错误", http.StatusBadRequest, KindClientError},
		{"422 是客户端错误", http.StatusUnprocessableEntity, KindClientError},
		{"404 成员无此模型应按可逃逸的瞬时失败", http.StatusNotFound, KindSoftTransient},
		{"405 方法不对应按瞬时失败", http.StatusMethodNotAllowed, KindSoftTransient},
		{"409 冲突应按瞬时失败", http.StatusConflict, KindSoftTransient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Classify(apiError(tc.status, "", false)))
		})
	}
	// routing-spec §4.1：404 这类失败必须能换人（否则会撞死在高优先级成员上）。
	assert.True(t, ShouldSwitchMember(Classify(apiError(http.StatusNotFound, "", false))))
	// 带 skipRetry 的是我方内部错误，仍按 client_error（不换人、不冷却）。
	assert.Equal(t, KindClientError, Classify(apiError(http.StatusNotFound, "", true)))
}

// 404 在尝试循环里应触发 failover，而不是不换人地结束请求。
func TestNotFoundFromMemberFailsOver(t *testing.T) {
	withFakeClock(t)
	state := NewState(testRoute("lane-p2-404-failover", 2, 1))

	_, _, ok := state.Next(nil)
	require.True(t, ok)
	member, _, ok := state.Next(apiError(http.StatusNotFound, "", false))
	require.True(t, ok, "404 应可逃逸到下一成员")
	assert.Equal(t, "channel-b", memberName(member))
}

// --- P2-2：亲和期绕过的高优先级成员必须留下 skipped 记录 ---

func TestAffinityBypassRecordsSkippedForHigherPriority(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-p2-affinity-skip", 2, 1)
	resolved.Config.MemberAffinitySeconds = 300
	state := NewState(resolved)

	// ch1 硬故障 → ch2；成功后启动亲和。
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	member, _, ok := state.Next(apiError(http.StatusUnauthorized, "", false))
	require.True(t, ok)
	require.Equal(t, "channel-b", memberName(member))
	state.OnSuccess()

	// 清除 ch1 冷却，使其"本可用"——此时只有亲和能解释"为什么没用 ch1"。
	runtime := Default.For(resolved.Model)
	runtime.withLock(func() {
		delete(runtime.Cooldowns, memberKeyOf(&resolved.Members[0]))
	})

	next := NewState(resolved)
	member, _, ok = next.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "亲和期内应继续用当前成员")

	statuses := map[string]string{}
	for _, a := range next.History() {
		statuses[a.Member] = a.Status
	}
	assert.Equal(t, "skipped", statuses["channel-a/model-1"],
		"亲和期绕过的高优先级成员必须留下 skipped 记录（routing-spec §6/§9）")
}

// --- P2-3：滚动窗口失败率参与熔断开断 ---

// 累计 Score 未达阈值，但近期窗口失败率达标时也必须开断（design-v1 §7.5）。
func TestRollingWindowFailureRateOpensCircuit(t *testing.T) {
	clock := withFakeClock(t)
	resolved := testRoute("lane-p2-rolling-open", 1, 1)
	resolved.Config.MemberCooldownSeconds = 1
	runtime := Default.For(resolved.Model)

	soft := apiError(http.StatusTooManyRequests, "", false) // 429 权重 0.2

	for i := 0; i < rollingMinSamples; i++ {
		state := NewState(resolved)
		_, _, ok := state.Next(nil)
		require.True(t, ok, "第 %d 次应拿到成员", i+1)
		state.Next(soft)
		clock.advance(1100 * time.Millisecond)
	}

	runtime.withLock(func() {
		circuit := runtime.Circuits[memberKeyOf(&resolved.Members[0])]
		require.NotNil(t, circuit)
		assert.Equal(t, CircuitOpen, circuit.State,
			"滚动窗口失败率达标也必须开断（design-v1 §7.5）")
		assert.Less(t, circuit.Score, DefaultCircuitSettings().FailureThreshold,
			"守卫：本例累计 Score 未达阈值，开断只能来自窗口失败率证据")
		assert.GreaterOrEqual(t, len(circuit.RecentOutcomes), rollingMinSamples)
	})
}

// 样本数不足时不得仅凭窗口失败率开断（保守性守卫）。
func TestRollingFailureRateNeedsMinimumSamples(t *testing.T) {
	clock := withFakeClock(t)
	resolved := testRoute("lane-p2-rolling-min", 1, 1)
	resolved.Config.MemberCooldownSeconds = 1
	runtime := Default.For(resolved.Model)
	soft := apiError(http.StatusTooManyRequests, "", false)

	for i := 0; i < rollingMinSamples-1; i++ {
		state := NewState(resolved)
		_, _, ok := state.Next(nil)
		require.True(t, ok)
		state.Next(soft)
		clock.advance(1100 * time.Millisecond)
	}
	runtime.withLock(func() {
		circuit := runtime.Circuits[memberKeyOf(&resolved.Members[0])]
		require.NotNil(t, circuit)
		assert.NotEqual(t, CircuitOpen, circuit.State,
			"样本数不足 rollingMinSamples 时不得仅凭窗口失败率开断")
	})
}

// --- P2-4：PruneStaleState 与配置热更新竞态 ---

// 顺序化复现竞态：旧配置请求（版本更旧）不得清理新配置刚写入的成员运行态。
func TestPruneStaleIgnoresOlderLaneVersion(t *testing.T) {
	withFakeClock(t)

	oldRoute := testRoute("lane-p2-stale-version", 1, 1)
	oldRoute.LaneVersion = 1
	NewState(oldRoute)
	runtime := Default.For(oldRoute.Model)

	// 新配置（版本 2，新增 ch2），先被观测并写入 ch2 的冷却。
	newRoute := testRoute("lane-p2-stale-version", 2, 1)
	newRoute.LaneVersion = 2
	NewState(newRoute)
	newKey := memberKeyOf(&newRoute.Members[1])
	runtime.withLock(func() {
		runtime.Cooldowns[newKey] = nowMs() + 60_000
	})

	// 旧配置请求（版本 1）随后到达：必须忽略，不得删除 ch2。
	NewState(oldRoute)
	runtime.withLock(func() {
		_, stillThere := runtime.Cooldowns[newKey]
		assert.True(t, stillThere, "旧配置请求不得删除新配置刚写入的成员运行态")
	})

	// 对照：更新版本（3）且成员集合缩小到 {ch1} 时，ch2 应被正常清理（不泄漏）。
	newer := testRoute("lane-p2-stale-version", 1, 1)
	newer.LaneVersion = 3
	NewState(newer)
	runtime.withLock(func() {
		_, gone := runtime.Cooldowns[newKey]
		assert.False(t, gone, "更新后的配置删除成员时应正常清理，不得泄漏")
	})
}

// 并发压测：新旧配置请求交错，旧配置请求绝不能清掉新成员状态。
func TestPruneStaleConcurrentConfigReloadSafe(t *testing.T) {
	withFakeClock(t)

	oldRoute := testRoute("lane-p2-stale-concurrent", 1, 1)
	oldRoute.LaneVersion = 1
	newRoute := testRoute("lane-p2-stale-concurrent", 2, 1)
	newRoute.LaneVersion = 2

	runtime := Default.For(oldRoute.Model)
	// 先让两条配置都被观测到，运行态进入"已处理版本 2"。
	NewState(oldRoute)
	NewState(newRoute)
	newKey := memberKeyOf(&newRoute.Members[1])

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			NewState(oldRoute) // 旧配置：必须 no-op
		}()
		go func() {
			defer wg.Done()
			NewState(newRoute)
			runtime.withLock(func() { runtime.Cooldowns[newKey] = nowMs() + 60_000 })
		}()
	}
	wg.Wait()

	runtime.withLock(func() {
		_, stillThere := runtime.Cooldowns[newKey]
		assert.True(t, stillThere, "并发旧配置请求不得清掉新成员状态")
	})
}

// 成员集合未变但版本前进时不应清理（config/mode 改动不涉及成员）。
func TestPruneStaleNoopWhenMemberSetUnchanged(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-p2-stale-same-set", 1, 1)
	resolved.LaneVersion = 1
	NewState(resolved)
	runtime := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])
	runtime.withLock(func() { runtime.Cooldowns[key] = nowMs() + 60_000 })

	// 版本前进，但成员集合完全相同：状态必须保留。
	bumped := testRoute("lane-p2-stale-same-set", 1, 1)
	bumped.LaneVersion = 2
	NewState(bumped)
	runtime.withLock(func() {
		_, stillThere := runtime.Cooldowns[key]
		assert.True(t, stillThere, "成员集合未变时不得清理（版本前进也不清）")
	})
}
