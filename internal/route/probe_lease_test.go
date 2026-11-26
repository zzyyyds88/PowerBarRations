package route

import (
	"sync"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/model"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemberKeyProjections(t *testing.T) {
	key := MemberKeyOf(&model.RouteMember{ChannelId: 7, UpstreamModel: "upstream", MemberId: 9})
	assert.Equal(t, "7:upstream#9", key.String())
	assert.Equal(t, "7:upstream", key.EventMember())
	assert.Equal(t, key.EventMember(), eventMember(key))
}

func TestProbeLeaseExactOwnershipExpiryAndABA(t *testing.T) {
	withFakeClock(t)
	runtime := newRuntime()
	key := MemberKey{ChannelID: 1, UpstreamModel: "m", MemberID: 2}
	old, ok := runtime.AcquireProbe(key, time.Second)
	require.True(t, ok)
	assert.True(t, runtime.ProbePending(key, nowMs()))
	assert.False(t, runtime.ReleaseProbe(nil))
	result := runtime.Reset()
	assert.Equal(t, uint64(1), result.Generation)
	assert.Equal(t, uint64(0), result.PreviousGeneration)
	assert.Equal(t, 1, result.InvalidatedProbeLeases)
	current, ok := runtime.AcquireProbe(key, time.Second)
	require.True(t, ok)
	assert.False(t, runtime.ReleaseProbe(old), "旧代次迟到释放不得命中新租约")
	assert.Same(t, current, runtime.probe)
	assert.True(t, runtime.ReleaseProbe(current))
	assert.False(t, runtime.ReleaseProbe(current))

	lease, ok := runtime.AcquireProbe(key, time.Second)
	require.True(t, ok)
	generationBeforeExpiry := runtime.generation
	nowMs = func() int64 { return lease.expiresAt }
	assert.False(t, runtime.ProbePending(key, nowMs()), "只读查询不得修改租约")
	assert.Same(t, lease, runtime.probe)
	replacement, ok := runtime.AcquireProbe(key, time.Second)
	require.True(t, ok, "到期租约由合法获取操作回收")
	assert.Equal(t, generationBeforeExpiry, runtime.generation, "租约到期不得推进全车道代次")
	assert.False(t, runtime.ReleaseProbe(lease))
	assert.Same(t, replacement, runtime.probe)
}

func TestProbeCleanupUsesPerLaneGenerationSnapshot(t *testing.T) {
	reg := &Registry{lanes: map[string]*Runtime{}}
	runtime := reg.For("lane-a")
	key := MemberKey{ChannelID: 1, UpstreamModel: "m", MemberID: 1}
	runtime.Cooldowns[key] = nowMs() + 60_000
	snapshot := reg.GenerationSnapshot()

	runtime.Reset()
	runtime.Cooldowns[key] = nowMs() + 60_000
	cleared := reg.ClearMemberPenaltyAtGenerations(key, snapshot)
	assert.Zero(t, cleared.Members, "Reset 后新代次不得被旧探活清理")
	assert.NotZero(t, runtime.Cooldowns[key])
}

func TestProbeLeaseDefaultTTLIsStreamSafe(t *testing.T) {
	runtime := newRuntime()
	lease, ok := runtime.AcquireProbe(MemberKey{ChannelID: 1, UpstreamModel: "m", MemberID: 1}, 0)
	require.True(t, ok)
	assert.Equal(t, DefaultProbeLeaseTTL.Milliseconds(), lease.expiresAt-lease.acquiredAt)
}

func TestProbeLeaseConcurrentAcquireRelease(t *testing.T) {
	runtime := newRuntime()
	key := MemberKey{ChannelID: 3, UpstreamModel: "concurrent", MemberID: 4}
	var wg sync.WaitGroup
	var mu sync.Mutex
	acquired := 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, ok := runtime.AcquireProbe(key, time.Minute)
			if ok {
				mu.Lock()
				acquired++
				mu.Unlock()
				runtime.ReleaseProbe(lease)
			}
		}()
	}
	wg.Wait()
	assert.Positive(t, acquired)
	assert.Nil(t, runtime.probe)
}

func TestNonOwnerOutcomesDoNotReleaseProbe(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lease-non-owner", 2, 1)
	resolved.Members[0].MemberId = 10
	resolved.Members[1].MemberId = 11
	owner := NewState(resolved)
	other := NewState(resolved)
	_, _, ok := owner.Next(nil)
	require.True(t, ok)
	key := MemberKeyOf(owner.Current())
	lease, acquired := owner.Runtime.AcquireProbe(key, time.Minute)
	require.True(t, acquired)
	owner.probe = lease

	other.Runtime.withLock(func() {
		other.Runtime.recordSuccess(MemberKeyOf(&resolved.Members[1]), 0, DefaultCircuitSettings(), true)
	})
	other.Runtime.withLock(func() {
		other.Runtime.recordFailure(MemberKeyOf(&resolved.Members[1]), KindSoftTransient, 5, 0, true, DefaultCircuitSettings())
	})
	assert.Same(t, lease, owner.Runtime.probe)
	assert.True(t, owner.Runtime.ReleaseProbe(lease))
}

func TestStateCloseIdempotentAndAfterCloseCleanup(t *testing.T) {
	state, lease := probeTestState(t)
	calls := 0
	state.AddCleanup(func() { calls++ })
	state.AddCleanup(func() { calls++ })
	state.Close()
	state.Close()
	assert.Nil(t, state.Runtime.probe)
	assert.Nil(t, lease.runtime.probe)
	assert.Equal(t, 2, calls)
	state.AddCleanup(func() { calls++ })
	assert.Equal(t, 3, calls)
}

func TestHealthIsReadOnlyWithExpiredLease(t *testing.T) {
	clock := withFakeClock(t)
	resolved := testRoute("lease-health-readonly", 1, 1)
	state := NewState(resolved)
	key := MemberKeyOf(&resolved.Members[0])
	lease, ok := state.Runtime.AcquireProbe(key, time.Second)
	require.True(t, ok)
	clock.advance(2 * time.Second)
	snapshot := Default.Health(resolved.Model, resolved, DefaultCircuitSettings())
	assert.False(t, snapshot.Members[0].Probing)
	assert.Nil(t, snapshot.Members[0].ProbeSince)
	assert.Nil(t, snapshot.Members[0].ProbeLeaseUntil)
	state.Runtime.withLock(func() { assert.Same(t, lease, state.Runtime.probe) })
}

func TestClearPenaltyExactNegativeZeroAndChannelScope(t *testing.T) {
	registry := &Registry{lanes: map[string]*Runtime{}}
	runtime := registry.For("scope")
	keys := []MemberKey{{ChannelID: 8, UpstreamModel: "m", MemberID: -1}, {ChannelID: 8, UpstreamModel: "m", MemberID: 0}, {ChannelID: 8, UpstreamModel: "m", MemberID: 1}}
	for _, key := range keys {
		runtime.Cooldowns[key] = nowMs() + 60000
	}
	result := registry.ClearMemberPenalty(keys[0])
	assert.Equal(t, 1, result.Members)
	assert.NotContains(t, runtime.Cooldowns, keys[0])
	assert.Contains(t, runtime.Cooldowns, keys[1])
	assert.Contains(t, runtime.Cooldowns, keys[2])
	result = registry.ClearChannelModelPenalty(8, "m")
	assert.Equal(t, 2, result.Members)
	assert.Empty(t, runtime.Cooldowns)
}

func TestResetRejectsInflightSuccessAndFailure(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lease-reset-inflight", 1, 1)
	state := NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	key := MemberKeyOf(&resolved.Members[0])
	result := state.Runtime.Reset()
	require.Equal(t, uint64(1), result.Generation)
	state.Runtime.Cooldowns[key] = nowMs() + 60000
	state.Runtime.Circuits[key] = &Circuit{State: CircuitOpen, Score: 7, OpenUntil: nowMs() + 60000}
	state.Runtime.HasCurrent = true
	state.Runtime.CurrentMember = key
	state.Runtime.AffinityUntil = nowMs() + 60000
	state.Runtime.laneDown = true
	state.OnSuccess()
	state.Runtime.withLock(func() {
		assert.Equal(t, nowMs()+60000, state.Runtime.Cooldowns[key])
		assert.Equal(t, float64(7), state.Runtime.Circuits[key].Score)
		assert.Equal(t, CircuitOpen, state.Runtime.Circuits[key].State)
		assert.Equal(t, nowMs()+60000, state.Runtime.AffinityUntil)
		assert.True(t, state.Runtime.laneDown)
	})
}

func TestResetRejectsInflightFailureButKeepsAttemptHistory(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lease-reset-failure", 1, 1)
	state := NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	state.Runtime.Reset()
	state.ReportMemberUnavailable(&resolved.Members[0], "late failure")
	require.Len(t, state.History(), 1)
	assert.Equal(t, "failed", state.History()[0].Status)
	state.Runtime.withLock(func() {
		assert.Empty(t, state.Runtime.Cooldowns)
		assert.Empty(t, state.Runtime.Circuits)
		assert.False(t, state.Runtime.laneDown)
	})
}

func TestLiveProbeNotMigratedWithMemberID(t *testing.T) {
	runtime := newRuntime()
	oldKey := MemberKey{ChannelID: 1, UpstreamModel: "m", MemberID: 1}
	newKey := MemberKey{ChannelID: 1, UpstreamModel: "m", MemberID: 2}
	lease, ok := runtime.AcquireProbe(oldKey, time.Minute)
	require.True(t, ok)
	runtime.PruneStaleState(map[MemberKey]bool{newKey: true}, 2)
	assert.Same(t, lease, runtime.probe)
	assert.Equal(t, oldKey, runtime.probe.key)
}
