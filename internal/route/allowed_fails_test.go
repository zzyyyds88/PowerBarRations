package route

import (
	"net/http"
	"testing"

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
func TestAllowedFailsOpensCircuitPerKind(t *testing.T) {
	withFakeClock(t)
	runtime := Default.For("lane-allowed-fails")
	key := "7:model-allowed"
	settings := DefaultCircuitSettings()
	settings.FailureThreshold = 100 // 分数阈值不可达
	settings.RollingMinSamples = 100
	settings.AllowedFails = map[ErrorKind]int{KindSoftRateLimit: 3}

	runtime.withLock(func() {
		for i := 0; i < 2; i++ {
			runtime.recordFailure(key, KindSoftRateLimit, 0, 0, false, settings)
		}
		require.NotNil(t, runtime.Circuits[key])
		assert.Equal(t, CircuitClosed, runtime.Circuits[key].State, "未达容忍次数不得开断")

		runtime.recordFailure(key, KindSoftRateLimit, 0, 0, false, settings)
		assert.Equal(t, CircuitOpen, runtime.Circuits[key].State, "达到该错误类型的容忍次数应开断")
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

// 上游 Retry-After 必须延长成员冷却，且不得超过 1 小时上限。
func TestRetryAfterExtendsMemberCooldown(t *testing.T) {
	clock := withFakeClock(t)
	resolved := testRoute("lane-retry-after", 2, 1)
	resolved.Config.MemberCooldownSeconds = 5
	state := NewState(resolved)

	_, _, ok := state.Next(nil)
	require.True(t, ok)
	rateLimited := apiError(http.StatusTooManyRequests, "", false)
	rateLimited.RetryAfterSeconds = 30
	_, _, ok = state.Next(rateLimited)
	require.True(t, ok, "429 应立即换成员")

	runtime := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])
	delta := runtime.Cooldowns[key] - clock.ms
	assert.GreaterOrEqual(t, delta, int64(30_000), "冷却不得短于 Retry-After")
	assert.LessOrEqual(t, delta, int64(36_000), "仅允许 0–20% 上偏抖动")
}

func TestRetryAfterCooldownIsCapped(t *testing.T) {
	withFakeClock(t)
	runtime := Default.For("lane-retry-after-cap")
	key := "9:model-cap"
	runtime.withLock(func() {
		runtime.recordFailure(key, KindSoftRateLimit, 5, 999_999, true, DefaultCircuitSettings())
	})
	delta := runtime.Cooldowns[key] - nowMs()
	assert.Equal(t, int64(maxMemberCooldownSeconds*1000), delta,
		"异常大的 Retry-After 必须封顶到 %d 秒", maxMemberCooldownSeconds)
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
