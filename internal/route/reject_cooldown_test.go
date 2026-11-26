package route

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 连续 upstream_reject 达阈值后进入短期冷却（routing-spec §4.1），
// 避免每个请求都先白打一个必然拒绝的成员；它不得计入熔断分。
func TestConsecutiveUpstreamRejectsApplyShortCooldown(t *testing.T) {
	clock := withFakeClock(t)
	resolved := testRoute("lane-reject-cooldown", 1, 1)
	resolved.Config.MemberCooldownSeconds = 1
	reject := apiError(http.StatusBadRequest, "", false)
	runtime := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])

	var before int64
	for i := 0; i < rejectStreakThreshold; i++ {
		if i == rejectStreakThreshold-1 {
			before = clock.ms
		}
		state := NewState(resolved)
		_, _, ok := state.Next(nil)
		require.True(t, ok, "第 %d 次请求应先拿到成员", i+1)
		_, _, ok = state.Next(reject)
		assert.False(t, ok, "唯一成员被拒绝后应无可用")
		clock.advance(100 * time.Millisecond)
	}

	runtime.withLock(func() {
		until := runtime.Cooldowns[key]
		// 冷却带 0–20% 确定性抖动并封顶（routing-spec §6），不能断言精确值。
		base := int64(rejectCooldownSeconds) * 1000
		upper := base + base/5
		assert.GreaterOrEqual(t, until, before+base,
			"连续拒绝应写入 reject_cooldown_seconds 的冷却")
		assert.LessOrEqual(t, until, before+upper,
			"拒绝冷却只允许 0–20% 上偏抖动")
		circuit := runtime.Circuits[key]
		require.NotNil(t, circuit)
		assert.Zero(t, circuit.Score, "上游拒绝不是成员故障，不得计入熔断分")
	})
}

// 「连续」必须被任何非 upstream_reject 结果打断（routing-spec §4.1）：
// reject → timeout → reject 只能算 1 次连续拒绝，不能累计成 2 次；
// 429 减速同理，它只是放慢，不是拒绝。
func TestNonRejectOutcomeBreaksRejectStreak(t *testing.T) {
	resolved := testRoute("lane-reject-streak", 1, 1)
	settings := DefaultCircuitSettings()
	runtime := Default.For(resolved.Model)
	key := memberKeyOf(&resolved.Members[0])

	runtime.withLock(func() {
		runtime.recordReject(key, KindUpstreamReject, true, false, DefaultCircuitSettings())
		runtime.recordReject(key, KindUpstreamReject, true, false, DefaultCircuitSettings())
		require.Equal(t, 2, runtime.Circuits[key].RejectStreak)
	})

	// timeout 是另一种失败，不是拒绝：序列到此中断。
	runtime.withLock(func() {
		runtime.recordFailure(key, KindTimeout, 0, 0, false, settings)
		require.Zero(t, runtime.Circuits[key].RejectStreak,
			"非 reject 失败必须清零连续拒绝计数")
	})
	runtime.withLock(func() {
		runtime.recordReject(key, KindUpstreamReject, true, false, DefaultCircuitSettings())
		assert.Equal(t, 1, runtime.Circuits[key].RejectStreak,
			"timeout 之后的拒绝必须从 1 重新计数")
	})

	// 429 只写 PacedUntil，不写冷却，也不是连续拒绝的一部分。
	runtime.withLock(func() {
		runtime.recordReject(key, KindUpstreamReject, true, false, DefaultCircuitSettings())
		require.Equal(t, 2, runtime.Circuits[key].RejectStreak)
		runtime.recordFailure(key, KindSoftRateLimit, 0, 5, false, settings)
		assert.Zero(t, runtime.Circuits[key].RejectStreak,
			"429 减速不是拒绝，必须清零连续计数")
	})
}
