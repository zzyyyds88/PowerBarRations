package route

import (
	"errors"
	"net/http"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 分类升级（routing-spec §4.1，ADR 0013） ---

// 200 状态码 + 上游错误对象 = 上游用 200 承载拒绝（非流式同形）。
func TestClassifyUpstreamErrorObjectAt200(t *testing.T) {
	openAI := types.WithOpenAIError(types.OpenAIError{Message: "blocked", Type: "upstream_error"}, http.StatusOK)
	assert.Equal(t, KindUpstreamReject, Classify(openAI), "OpenAI 错误对象形态")

	claude := types.WithClaudeError(types.ClaudeError{Message: "blocked"}, http.StatusOK)
	assert.Equal(t, KindUpstreamReject, Classify(claude), "Claude 错误对象形态也必须识别")
}

func TestClassifyNewStatusCodes(t *testing.T) {
	cases := []struct {
		status int
		want   ErrorKind
	}{
		{http.StatusPaymentRequired, KindHardQuota},
		{http.StatusRequestEntityTooLarge, KindClientError},
		{http.StatusUnavailableForLegalReasons, KindUpstreamReject},
		{499, KindCanceled},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, Classify(apiError(tc.status, "", false)), "status=%d", tc.status)
	}
}

// 我方构造的坏请求（skipRetry）必须优先于错误码判定：否则 BadResponse 这类
// 内部错误会被当成上游故障，冷却健康成员（routing-spec §4.1.2）。
func TestClassifySkipRetryBeatsErrorCode(t *testing.T) {
	for _, code := range []types.ErrorCode{types.ErrorCodeBadResponse, types.ErrorCodeDoRequestFailed} {
		err := types.NewError(errors.New("bad body"), code,
			types.ErrOptionWithStatusCode(http.StatusInternalServerError),
			types.ErrOptionWithSkipRetry())
		assert.Equal(t, KindClientError, Classify(err), "code=%s 的 skipRetry 错误必须是 client_error", code)
	}
}

// --- 运行态键含 memberId（ADR 0009 修订）：重复成员各自独立 ---

func TestDuplicateMembersHaveIndependentRuntime(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-dup-runtime", 1, 1)
	second := resolved.Members[0]
	resolved.Members[0].MemberId = 1
	second.MemberId = 2
	second.Priority = 99
	resolved.Members = append(resolved.Members, second)
	state := NewState(resolved)

	firstKey := memberKeyOf(&resolved.Members[0])
	secondKey := memberKeyOf(&resolved.Members[1])
	require.NotEqual(t, firstKey, secondKey, "同一 (渠道,模型) 的不同成员必须有不同运行态键")

	state.Runtime.withLock(func() {
		state.Runtime.recordFailure(firstKey, KindHardAuth, 60, 0, true, DefaultCircuitSettings())
	})

	snapshot := state.Runtime.Health(resolved, CurrentCircuitSettings())
	require.Len(t, snapshot.Members, 2)
	assert.NotNil(t, snapshot.Members[0].CooldownUntil, "仅失败的成员进入冷却")
	assert.Nil(t, snapshot.Members[1].CooldownUntil, "同渠道同模型的另一成员不得继承冷却")
}

// --- 429 减速不改变"可选性"（routing-spec §4.1.1/§5.3） ---

func TestPaceDoesNotMakeMemberUnavailable(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-pace-available", 1, 1)
	state := NewState(resolved)
	key := memberKeyOf(&resolved.Members[0])

	state.Runtime.withLock(func() {
		state.Runtime.PacedUntil[key] = nowMs() + 30_000
		assert.Equal(t, availOK, state.Runtime.availabilityOf(key, CurrentCircuitSettings()),
			"减速只影响发送时机，不是不可用")
	})

	snapshot := state.Runtime.Health(resolved, CurrentCircuitSettings())
	require.Len(t, snapshot.Members, 1)
	assert.True(t, snapshot.Members[0].Available, "限流减速期间成员仍报告 available")
	assert.NotEmpty(t, snapshot.Members[0].PacedUntil, "健康快照必须暴露 paced_until")
	assert.Nil(t, snapshot.Members[0].CooldownUntil, "429 不得写 cooldown_until")
}

// --- 健康快照新字段与探测槽语义（routing-spec §7） ---

func TestHealthExposesLastErrorAndProbePending(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-health-v2", 1, 1)
	state := NewState(resolved)
	key := memberKeyOf(&resolved.Members[0])

	state.Runtime.withLock(func() {
		state.Runtime.recordFailure(key, KindSoftTransient, 5, 0, false, DefaultCircuitSettings())
		state.Runtime.Circuits[key].State = CircuitOpen
		state.Runtime.Circuits[key].OpenUntil = nowMs() - 1
		state.Runtime.probe = &ProbeLease{expiresAt: nowMs() + 60000}
		state.Runtime.probe = &ProbeLease{key: key, expiresAt: nowMs() + 60000}
	})
	snapshot := state.Runtime.Health(resolved, CurrentCircuitSettings())
	require.Len(t, snapshot.Members, 1)
	assert.NotEmpty(t, snapshot.Members[0].LastErrorAt, "必须暴露最近错误时间")
	assert.Equal(t, KindSoftTransient, snapshot.Members[0].LastErrorKind)
	assert.GreaterOrEqual(t, snapshot.Members[0].RollingSamples, 1, "滚动窗口样本数必须可观测")
	assert.True(t, snapshot.Members[0].Probing, "半开探测在途时 probe_pending=true")
	assert.False(t, snapshot.Members[0].Available, "探测槽被占用时该成员不再对外报告可用")
}

// 探测槽是车道级单槽：别的成员在探测时，本成员只是"等槽"，不得标成 probe_pending。
func TestProbePendingOnlyForSlotOwner(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-probe-owner", 2, 1)
	state := NewState(resolved)
	firstKey := memberKeyOf(&resolved.Members[0])
	secondKey := memberKeyOf(&resolved.Members[1])

	state.Runtime.withLock(func() {
		now := nowMs()
		state.Runtime.Circuits[firstKey] = &Circuit{State: CircuitOpen, OpenUntil: now - 1}
		state.Runtime.Circuits[secondKey] = &Circuit{State: CircuitOpen, OpenUntil: now - 1}
		state.Runtime.probe = &ProbeLease{expiresAt: nowMs() + 60000}
		state.Runtime.probe = &ProbeLease{key: firstKey, expiresAt: nowMs() + 60000}
	})

	snapshot := state.Runtime.Health(resolved, CurrentCircuitSettings())
	require.Len(t, snapshot.Members, 2)
	assert.True(t, snapshot.Members[0].Probing, "持槽成员 probe_pending=true")
	assert.False(t, snapshot.Members[1].Probing, "等槽成员不得标成探测中")
	assert.False(t, snapshot.Members[1].Available, "探测槽被占用时等槽成员本轮不可选")
}

// --- Reset 结构化明细 + 清理减速/停摆标记 ---

func TestResetClearsPaceAndLaneDown(t *testing.T) {
	withFakeClock(t)
	runtime := freshRuntime(t, "lane-reset-v2")
	key := testMemberKey("13:model-reset-v2")
	runtime.withLock(func() {
		runtime.PacedUntil[key] = nowMs() + 30_000
		runtime.Cooldowns[key] = nowMs() + 60_000
		runtime.Circuits[key] = &Circuit{State: CircuitOpen, OpenUntil: nowMs() + 60_000}
		runtime.noteLaneUnavailable()
		runtime.HasCurrent = true
		runtime.CurrentMember = key
	})

	result := runtime.Reset()
	assert.Equal(t, ResetResult{Circuits: 1, Cooldowns: 1, Paces: 1, LaneDown: true, Generation: 1}, result)
	runtime.withLock(func() {
		assert.Empty(t, runtime.PacedUntil)
		assert.Empty(t, runtime.Cooldowns)
		assert.Empty(t, runtime.Circuits)
		assert.False(t, runtime.laneDown)
		assert.Zero(t, runtime.laneDownSince)
		assert.False(t, runtime.HasCurrent)
		assert.Empty(t, runtime.CurrentMember)
	})
}

// --- 探活清惩罚态（routing-spec §5.4） ---

func TestClearMemberPenaltyAcrossLanes(t *testing.T) {
	withFakeClock(t)
	key := func(memberId int) MemberKey {
		return MemberKey{ChannelID: 14, UpstreamModel: "model-probe", MemberID: memberId}
	}

	laneA := freshRuntime(t, "lane-probe-clear-a")
	laneB := freshRuntime(t, "lane-probe-clear-b")
	for _, runtime := range []*Runtime{laneA, laneB} {
		runtime.withLock(func() {
			runtime.Cooldowns[key(1)] = nowMs() + 60_000
			runtime.PacedUntil[key(1)] = nowMs() + 30_000
			runtime.Circuits[key(1)] = &Circuit{
				State: CircuitOpen, OpenUntil: nowMs() + 60_000,
				Score: 3, ConsecutiveFailures: 2, LastErrorKind: KindHardAuth,
			}
		})
	}
	// laneB 的成员探测槽在途：必须跳过并报出，不得破坏单探测槽不变量。
	laneB.withLock(func() {
		laneB.probe = &ProbeLease{expiresAt: nowMs() + 60000}
		laneB.probe = &ProbeLease{key: key(1), expiresAt: nowMs() + 60000}
	})

	cleared := Default.ClearMemberPenalty(MemberKey{ChannelID: 14, UpstreamModel: "model-probe", MemberID: 1})
	assert.Equal(t, []string{"lane-probe-clear-a"}, cleared.Lanes)
	assert.Equal(t, 1, cleared.Members)
	assert.Equal(t, []string{"lane-probe-clear-b"}, cleared.ProbeBusyLanes)

	laneA.withLock(func() {
		assert.Zero(t, laneA.Cooldowns[key(1)])
		assert.Zero(t, laneA.PacedUntil[key(1)])
		assert.Equal(t, CircuitClosed, laneA.Circuits[key(1)].State)
		assert.Zero(t, laneA.Circuits[key(1)].Score)
		assert.Zero(t, laneA.Circuits[key(1)].ConsecutiveFailures)
	})
	laneB.withLock(func() {
		assert.NotZero(t, laneB.Cooldowns[key(1)], "探测槽在途时该车道不得被清理")
	})
}

func TestClearMemberPenaltyDropsProbeBusyFlagAfterRelease(t *testing.T) {
	withFakeClock(t)
	key := testMemberKey("15:model-probe#2")
	runtime := freshRuntime(t, "lane-probe-clear-release")
	runtime.withLock(func() {
		runtime.Cooldowns[key] = nowMs() + 60_000
		runtime.probe = &ProbeLease{expiresAt: nowMs() + 60000}
		runtime.probe = &ProbeLease{key: key, expiresAt: nowMs() + 60000}
	})
	first := Default.ClearMemberPenalty(MemberKey{ChannelID: 15, UpstreamModel: "model-probe", MemberID: 2})
	assert.Empty(t, first.Lanes)
	assert.Equal(t, []string{"lane-probe-clear-release"}, first.ProbeBusyLanes)

	runtime.withLock(func() {
		runtime.probe = nil
	})
	second := Default.ClearMemberPenalty(MemberKey{ChannelID: 15, UpstreamModel: "model-probe", MemberID: 2})
	assert.Equal(t, []string{"lane-probe-clear-release"}, second.Lanes)
	assert.Equal(t, 1, second.Members)
}

// 渠道级探活没有 memberId：清理该 (channel, model) 下的全部成员条目。
func TestClearMemberPenaltyChannelScopeClearsAllMemberIDs(t *testing.T) {
	withFakeClock(t)
	runtime := freshRuntime(t, "lane-probe-clear-channel")
	for _, memberId := range []int{1, 2} {
		key := MemberKey{ChannelID: 16, UpstreamModel: "model-shared", MemberID: memberId}
		runtime.withLock(func() {
			runtime.Cooldowns[key] = nowMs() + 60_000
		})
	}
	cleared := Default.ClearChannelModelPenalty(16, "model-shared")
	assert.Contains(t, cleared.Lanes, "lane-probe-clear-channel")
	assert.Equal(t, 2, cleared.Members)
}

// 渠道停用属配置态跳过：不写冷却、不计熔断分（routing-spec §5.3）。
func TestReportMemberConfigSkipIsNotFailure(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute(t, "lane-config-skip", 2, 2)
	resolved.Members[0].ChannelEnabled = false
	state := NewState(resolved)
	member, _, ok := state.Next(nil)
	require.True(t, ok)
	require.NotNil(t, member)
	key := memberKeyOf(member)

	state.ReportMemberConfigSkip(member, "channel disabled or missing")
	state.Runtime.withLock(func() {
		assert.Zero(t, state.Runtime.Cooldowns[key], "渠道停用不得写冷却")
		circuit := state.Runtime.Circuits[key]
		if circuit != nil {
			assert.Zero(t, circuit.Score, "渠道停用不得计熔断分")
			assert.Zero(t, circuit.ConsecutiveFailures)
		}
	})
	statuses := map[string]string{}
	for _, attempt := range state.History() {
		statuses[attempt.Member] = attempt.Status
	}
	assert.Equal(t, "channel_disabled", statuses[memberLabel(member)])

	// 配置跳过耗尽本请求内该成员预算，下一轮必须换人。
	next, _, ok := state.Next(nil)
	require.True(t, ok)
	assert.NotEqual(t, key, memberKeyOf(next))
}

// 车道整链替换会换 memberId：清理旧键前必须把唯一同源成员的运行态迁到新键，
// 否则"打开编辑再保存"会清空冷却/熔断（L2 runbook 3d）。
func TestPruneMigratesRuntimeAcrossMemberIdChange(t *testing.T) {
	withFakeClock(t)
	runtime := freshRuntime(t, "lane-prune-migrate")
	oldKey := testMemberKey("20:model-x#1")
	newKey := testMemberKey("20:model-x#2")
	until := nowMs() + 120_000
	runtime.withLock(func() {
		runtime.Cooldowns[oldKey] = until
		runtime.PacedUntil[oldKey] = until
		runtime.Circuits[oldKey] = &Circuit{State: CircuitOpen, OpenUntil: until, ConsecutiveFailures: 1}
		runtime.HasCurrent = true
		runtime.CurrentMember = oldKey
		runtime.pruneStaleLocked(map[MemberKey]bool{newKey: true})
		assert.Equal(t, until, runtime.Cooldowns[newKey])
		assert.Equal(t, until, runtime.PacedUntil[newKey])
		require.NotNil(t, runtime.Circuits[newKey])
		assert.Equal(t, CircuitOpen, runtime.Circuits[newKey].State)
		assert.Equal(t, newKey, runtime.CurrentMember)
		assert.Zero(t, runtime.Cooldowns[oldKey])
		assert.Nil(t, runtime.Circuits[oldKey])
	})
}

// 重复成员（同渠道同模型多条）无法判定旧键归属：不得迁移，宁可丢也不串。
func TestPruneDoesNotMigrateAmbiguousDuplicateMembers(t *testing.T) {
	withFakeClock(t)
	runtime := freshRuntime(t, "lane-prune-ambiguous")
	oldKey := testMemberKey("21:model-y#1")
	valid := map[MemberKey]bool{testMemberKey("21:model-y#2"): true, testMemberKey("21:model-y#3"): true}
	runtime.withLock(func() {
		runtime.Cooldowns[oldKey] = nowMs() + 120_000
		runtime.pruneStaleLocked(valid)
		assert.Zero(t, runtime.Cooldowns[oldKey])
		assert.Empty(t, runtime.Cooldowns)
	})
}
