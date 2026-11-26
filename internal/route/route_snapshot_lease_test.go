package route

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/model"
	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
)

func TestRouteSnapshotSignatureIncludesChannelAndAdmissionConfig(t *testing.T) {
	base := testRoute("snapshot-fields", 1, 1)
	base.Members[0].MemberId = 1
	base.Members[0].ChannelVersion = 1
	signature := routeSnapshotSignature(base)
	mutations := map[string]func(*model.ResolvedRoute){
		"enabled": func(r *model.ResolvedRoute) { r.Members[0].ChannelEnabled = !r.Members[0].ChannelEnabled },
		"version": func(r *model.ResolvedRoute) { r.Members[0].ChannelVersion++ },
		"config error": func(r *model.ResolvedRoute) {
			r.Members[0].ConfigError = &model.ChannelConfigError{Code: "invalid", Message: "bad", Path: "/setting"}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := *base
			changed.Members = append([]model.RouteMember(nil), base.Members...)
			mutate(&changed)
			assert.NotEqual(t, signature, routeSnapshotSignature(&changed))
		})
	}
	for name, mutate := range map[string]func(*model.RouteMember){
		"model":       func(m *model.RouteMember) { m.RateLimit.Model = "other" },
		"concurrency": func(m *model.RouteMember) { m.RateLimit.MaxConcurrency = 9 },
		"rpm":         func(m *model.RouteMember) { m.RateLimit.MaxRPM = 8 },
		"queue":       func(m *model.RouteMember) { m.RateLimit.QueueLimit = 7 },
		"wait":        func(m *model.RouteMember) { m.RateLimit.QueueWaitSeconds = 6 },
	} {
		t.Run("rate "+name, func(t *testing.T) {
			changed := *base
			changed.Members = append([]model.RouteMember(nil), base.Members...)
			changed.Members[0].RateLimit = &dto.ChannelRateLimit{Model: "model-1", MaxConcurrency: 2, MaxRPM: 3, QueueLimit: 4, QueueWaitSeconds: 5}
			sig := routeSnapshotSignature(&changed)
			mutate(&changed.Members[0])
			assert.NotEqual(t, sig, routeSnapshotSignature(&changed))
		})
	}
}

func TestPruneThenResetDoesNotReviveOldLease(t *testing.T) {
	runtime := newRuntime()
	oldKey := MemberKey{ChannelID: 1, UpstreamModel: "m", MemberID: 1}
	newKey := MemberKey{ChannelID: 1, UpstreamModel: "m", MemberID: 2}
	lease, ok := runtime.AcquireProbe(oldKey, 60_000_000_000)
	require.True(t, ok)
	runtime.PruneStaleState(map[MemberKey]bool{newKey: true}, 2)
	assert.Same(t, lease, runtime.probe)
	result := runtime.Reset()
	assert.Equal(t, 1, result.InvalidatedProbeLeases)
	replacement, ok := runtime.AcquireProbe(newKey, 60_000_000_000)
	require.True(t, ok)
	assert.False(t, runtime.ReleaseProbe(lease))
	assert.Same(t, replacement, runtime.probe)
}
