package route

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 探测槽归属：未占槽的并发请求不得释放别人的槽，否则"每车道同时只放行一个探测"
// 在并发下失效（routing-spec §2.2/§6，审查 F4）。
func TestReleaseProbeOnlyReleasesOwnSlot(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-probe-owner", 2, 1)
	runtime := Default.For(resolved.Model)
	// 成员 a 冷却已到期 → 下次请求会以"探测"身份选中它。
	runtime.withLock(func() {
		runtime.Cooldowns[memberKeyOf(&resolved.Members[0])] = nowMs() - 1
	})

	prober := NewState(resolved)
	member, _, ok := prober.Next(nil)
	require.True(t, ok)
	require.Equal(t, "channel-a", memberName(member))
	probeKey := memberKeyOf(&resolved.Members[0])
	runtime.withLock(func() {
		require.True(t, runtime.HasProbe)
		require.Equal(t, probeKey, runtime.ProbeMember)
	})
	require.Equal(t, probeKey, prober.claimedProbe)

	// 另一个请求只是普通选路（没有占槽），收尾时归还探测槽必须无效。
	other := NewState(resolved)
	member, _, ok = other.Next(nil)
	require.True(t, ok)
	require.Equal(t, "channel-b", memberName(member), "槽被占，其他请求落到下一成员")
	require.Empty(t, other.claimedProbe)
	other.ReleaseProbe()
	runtime.withLock(func() {
		assert.True(t, runtime.HasProbe, "非持有者归还后，槽必须仍被探测者占着")
		assert.Equal(t, probeKey, runtime.ProbeMember)
	})

	// 第三个请求仍不得拿到探测位。
	third := NewState(resolved)
	member, _, ok = third.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member))

	// 持有者收尾才真正归还。
	prober.ReleaseProbe()
	runtime.withLock(func() {
		assert.False(t, runtime.HasProbe)
	})
	fourth := NewState(resolved)
	member, _, ok = fourth.Next(nil)
	require.True(t, ok)
	assert.Equal(t, "channel-a", memberName(member), "归还后应能再次放行探测")
	assert.Equal(t, probeKey, fourth.claimedProbe)
}

// 失败换人后，本请求对旧成员的探测占用必须一并释放并清空归属。
func TestProbeOwnershipClearedAfterFailover(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-probe-failover", 2, 1)
	resolved.Config.MemberCooldownSeconds = 5
	runtime := Default.For(resolved.Model)
	runtime.withLock(func() {
		runtime.Cooldowns[memberKeyOf(&resolved.Members[0])] = nowMs() - 1
	})

	state := NewState(resolved)
	member, _, ok := state.Next(nil)
	require.True(t, ok)
	require.Equal(t, "channel-a", memberName(member))
	require.NotEmpty(t, state.claimedProbe)

	soft := apiError(http.StatusInternalServerError, "", false)
	member, _, ok = state.Next(soft)
	require.True(t, ok)
	assert.Equal(t, "channel-b", memberName(member), "探测失败后换到下一成员")
	assert.Empty(t, state.claimedProbe, "换人后归属必须清空")
	runtime.withLock(func() {
		assert.False(t, runtime.HasProbe, "换人时探测槽必须已归还")
	})
}

// 成功路径同样归还探测槽并清空归属。
func TestProbeOwnershipClearedOnSuccess(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-probe-success", 1, 1)
	runtime := Default.For(resolved.Model)
	runtime.withLock(func() {
		runtime.Cooldowns[memberKeyOf(&resolved.Members[0])] = nowMs() - 1
	})

	state := NewState(resolved)
	_, _, ok := state.Next(nil)
	require.True(t, ok)
	require.NotEmpty(t, state.claimedProbe)

	state.OnSuccess()
	assert.Empty(t, state.claimedProbe)
	runtime.withLock(func() {
		assert.False(t, runtime.HasProbe)
	})
}

// 我方超时被翻译成"同时命中 DeadlineExceeded 与 Canceled"的错误链后，
// 分类必须判为真实失败（soft_transient），不得退化成 canceled（审查 F1）。
func TestClassifyPrefersDeadlineOverCanceled(t *testing.T) {
	wrapped := fmt.Errorf("%w: pbr attempt timeout: %w", context.DeadlineExceeded, context.Canceled)
	assert.True(t, errors.Is(wrapped, context.DeadlineExceeded))
	assert.True(t, errors.Is(wrapped, context.Canceled))

	err := types.NewError(wrapped, types.ErrorCodeDoRequestFailed)
	assert.Equal(t, KindSoftTransient, Classify(err))
	assert.True(t, ShouldSwitchMember(Classify(err)), "超时必须换人")
	assert.True(t, ShouldRetrySameMember(Classify(err)), "软故障在原成员预算内先重试")

	// 纯客户端取消仍然不乱换人。
	pureCancel := types.NewError(fmt.Errorf("read: %w", context.Canceled), types.ErrorCodeDoRequestFailed)
	assert.Equal(t, KindCanceled, Classify(pureCancel))
	assert.False(t, ShouldSwitchMember(Classify(pureCancel)))
}
