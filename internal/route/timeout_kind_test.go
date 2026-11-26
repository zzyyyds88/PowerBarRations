package route

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// timeout 与 soft_transient 同档：allowed_fails 默认一致、可独立配置（routing-spec §4.1）。
func TestTimeoutSharesSoftCooldownTier(t *testing.T) {
	defaults, err := ParseAllowedFails("")
	require.NoError(t, err)
	assert.Equal(t, defaults[KindSoftTransient], defaults[KindTimeout], "timeout 与 soft_transient 同默认容忍次数")

	parsed, err := ParseAllowedFails(`{"timeout":2}`)
	require.NoError(t, err)
	assert.Equal(t, 2, parsed[KindTimeout], "timeout 必须是可配置键")
}

// timeout 的分级冷却与 soft_transient 一致：n=1 → base/8（下限 5s），
// 而非满档冷却——上游高峰期的单次超时不该把成员锁掉整段 60s。
func TestTimeoutCooldownScalesLikeSoftTransient(t *testing.T) {
	withFakeClock(t)
	runtime := Default.For("lane-timeout-cooldown")
	key := testMemberKey("9:model-timeout")

	runtime.withLock(func() {
		runtime.recordFailure(key, KindTimeout, 60, 0, true, DefaultCircuitSettings())
		cooldownUntil := runtime.Cooldowns[key]
		require.NotZero(t, cooldownUntil)
		scaled := cooldownUntil - nowMs()
		assert.GreaterOrEqual(t, scaled, int64(5_000), "分级下限 5s")
		assert.LessOrEqual(t, scaled, int64(8_400), "n=1 → 60/8=7s，加 0–20% 抖动上限 8.4s")
	})
}
