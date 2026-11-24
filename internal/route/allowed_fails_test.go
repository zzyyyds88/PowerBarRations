package route

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAllowedFails(t *testing.T) {
	defaults, err := ParseAllowedFails("")
	require.NoError(t, err)
	assert.Equal(t, 2, defaults[KindHardAuth])
	assert.Equal(t, 10, defaults[KindSoftRateLimit])

	parsed, err := ParseAllowedFails(`{"hard_auth":3,"soft_rate_limit":7}`)
	require.NoError(t, err)
	assert.Equal(t, map[ErrorKind]int{KindHardAuth: 3, KindSoftRateLimit: 7}, parsed)

	_, err = ParseAllowedFails(`{"unknown_kind":1}`)
	assert.Error(t, err, "未知错误类型必须报错，不能静默忽略")
	_, err = ParseAllowedFails(`{"hard_auth":0}`)
	assert.Error(t, err, "容忍次数必须为正")
	_, err = ParseAllowedFails(`not-json`)
	assert.Error(t, err)
}

// allowed_fails 按错误类型独立开断：即使累计分/滚动窗口都未达标。
// 注意 soft_rate_limit 不参与判罚（routing-spec §4.1.1），这里用 soft_transient 验证机制本身。
func TestAllowedFailsOpensCircuitPerKind(t *testing.T) {
	withFakeClock(t)
	runtime := Default.For("lane-allowed-fails")
	key := "7:model-allowed"
	settings := DefaultCircuitSettings()
	settings.FailureThreshold = 100 // 分数阈值不可达
	settings.RollingMinSamples = 100
	settings.AllowedFails = map[ErrorKind]int{KindSoftTransient: 3}

	runtime.withLock(func() {
		for i := 0; i < 2; i++ {
			runtime.recordFailure(key, KindSoftTransient, 0, 0, false, settings)
		}
		require.NotNil(t, runtime.Circuits[key])
		assert.Equal(t, CircuitClosed, runtime.Circuits[key].State, "未达容忍次数不得开断")

		runtime.recordFailure(key, KindSoftTransient, 0, 0, false, settings)
		assert.Equal(t, CircuitOpen, runtime.Circuits[key].State, "达到该错误类型的容忍次数应开断")
	})
}

// 429 只减速、不判罚：即使 allowed_fails 配置成 1，也不能打开熔断或写冷却。
func TestRateLimitNeverOpensCircuitOrCooldown(t *testing.T) {
	withFakeClock(t)
	runtime := Default.For("lane-rate-limit-no-fault")
	key := "7:model-paced"
	settings := DefaultCircuitSettings()
	settings.AllowedFails = map[ErrorKind]int{KindSoftRateLimit: 1}

	runtime.withLock(func() {
		for i := 0; i < 5; i++ {
			runtime.recordFailure(key, KindSoftRateLimit, 5, 0, true, settings)
		}
		circuit := runtime.Circuits[key]
		require.NotNil(t, circuit)
		assert.Equal(t, CircuitClosed, circuit.State, "429 不得打开熔断")
		assert.Zero(t, circuit.Score, "429 不得计入熔断分")
		assert.Zero(t, circuit.ConsecutiveFailures, "429 不得计入连续失败")
		assert.Empty(t, circuit.FailsByKind, "429 不得计入按类型失败数")
		assert.NotZero(t, runtime.PacedUntil[key], "429 必须写限流减速")
		assert.Empty(t, runtime.Cooldowns, "429 不得写冷却")
	})
}

// 成功会清零按类型的连续失败计数，避免"跨恢复周期累计"误开断。
func TestSuccessResetsAllowedFailStreak(t *testing.T) {
	withFakeClock(t)
	runtime := Default.For("lane-allowed-reset")
	key := "8:model-reset"
	settings := DefaultCircuitSettings()
	settings.FailureThreshold = 100
	settings.RollingMinSamples = 100
	settings.AllowedFails = map[ErrorKind]int{KindHardAuth: 3}

	runtime.withLock(func() {
		for i := 0; i < 2; i++ {
			runtime.recordFailure(key, KindHardAuth, 0, 0, false, settings)
		}
		runtime.recordSuccess(key, 0, settings)
		for i := 0; i < 2; i++ {
			runtime.recordFailure(key, KindHardAuth, 0, 0, false, settings)
		}
		assert.Equal(t, CircuitClosed, runtime.Circuits[key].State, "成功后应重新计数")
	})
}

// 上游 429 的 Retry-After 必须成为该成员的减速截止时间：不写冷却、
// 原地重试必须等满 Retry-After（routing-spec §4.1.1/§3.1）。
func TestRetryAfterSetsMemberPace(t *testing.T) {
	clock := withFakeClock(t)
	// 每次选中即消耗 1 点尝试预算；给 2 点才能观察"429 原地重试"。
	resolved := testRoute("lane-retry-after", 2, 2)
	resolved.Config.MemberCooldownSeconds = 5
	state := NewState(resolved)

	_, _, ok := state.Next(nil)
	require.True(t, ok)
	rateLimited := apiError(http.StatusTooManyRequests, "", false)
	rateLimited.RetryAfterSeconds = 30
	member, wait, ok := state.Next(rateLimited)
	require.True(t, ok, "预算未耗尽时应原地重试")
	require.NotNil(t, member)
	assert.Equal(t, 30*time.Second, wait, "重试等待必须等满 Retry-After")

	runtime := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])
	runtime.withLock(func() {
		delta := runtime.PacedUntil[key] - clock.ms
		assert.Equal(t, int64(30_000), delta, "减速截止 = now + Retry-After（不抖动）")
		assert.Zero(t, runtime.Cooldowns[key], "429 不得写冷却")
	})
}

// 非 429 的 Retry-After 仍然延长成员冷却（先本地分级、再取较大值、最后封顶+抖动）。
func TestRetryAfterExtendsMemberCooldown(t *testing.T) {
	clock := withFakeClock(t)
	runtime := Default.For("lane-retry-after-cooldown")
	key := "10:model-cooldown"
	runtime.withLock(func() {
		runtime.recordFailure(key, KindSoftTransient, 5, 30, true, DefaultCircuitSettings())
	})
	delta := runtime.Cooldowns[key] - clock.ms
	assert.GreaterOrEqual(t, delta, int64(30_000), "冷却不得短于 Retry-After")
	assert.LessOrEqual(t, delta, int64(36_000), "仅允许 0–20% 上偏抖动")
}

func TestRetryAfterCooldownIsCapped(t *testing.T) {
	withFakeClock(t)
	runtime := Default.For("lane-retry-after-cap")
	key := "9:model-cap"
	runtime.withLock(func() {
		runtime.recordFailure(key, KindSoftTransient, 5, 999_999, true, DefaultCircuitSettings())
	})
	delta := runtime.Cooldowns[key] - nowMs()
	assert.Equal(t, int64(maxMemberCooldownSeconds*1000), delta,
		"异常大的 Retry-After 必须封顶到 %d 秒", maxMemberCooldownSeconds)
}

// 429 的减速同样有上限：上游给再大的 Retry-After 也不能把成员无限期钉住。
func TestRateLimitPaceIsCapped(t *testing.T) {
	withFakeClock(t)
	runtime := Default.For("lane-pace-cap")
	key := "9:model-pace-cap"
	runtime.withLock(func() {
		runtime.recordFailure(key, KindSoftRateLimit, 5, 999_999, true, DefaultCircuitSettings())
	})
	delta := runtime.PacedUntil[key] - nowMs()
	assert.Equal(t, int64(rateLimitMaxPaceSeconds*1000), delta,
		"异常大的 Retry-After 必须封顶到 %d 秒", rateLimitMaxPaceSeconds)
}

func TestCooldownJitterStaysWithin20Percent(t *testing.T) {
	const base = 100
	for i := 0; i < 50; i++ {
		key := "member-" + string(rune('a'+i%26)) + "-" + string(rune('0'+i/26))
		got := jitterSeconds(base, key, int64(i))
		assert.GreaterOrEqual(t, got, base)
		assert.LessOrEqual(t, got, base+base/5)
	}
	assert.Equal(t, 1, jitterSeconds(1, "tiny", 0), "1 秒不再放大")
}
