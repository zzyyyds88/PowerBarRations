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
		assert.Equal(t, before+int64(rejectCooldownSeconds)*1000, until,
			"连续拒绝应写入 reject_cooldown_seconds 的冷却")
		circuit := runtime.Circuits[key]
		require.NotNil(t, circuit)
		assert.Zero(t, circuit.Score, "上游拒绝不是成员故障，不得计入熔断分")
	})
}
