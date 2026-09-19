package route

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// api-spec §6.5 / §2.6：车道健康快照的时间字段是 RFC3339 UTC 字符串，无冷却/无
// 退避为 null；亲和是对象（不是 affinity_until 的 unix 秒）。前端（web/src/lib/
// route-events.ts）按此形状消费，形状回退会让运行态列静默坏掉。

// 时间字段必须是 RFC3339 UTC 字符串，且能被 JSON 解析回同一时刻。
func TestHealthSnapshotTimesAreRFC3339(t *testing.T) {
	clock := withFakeClock(t)
	resolved := testRoute("lane-health-rfc3339", 2, 1)
	state := NewState(resolved)
	key := memberKeyOf(&resolved.Members[0])

	cooldownUntil := nowMs() + 60_000
	state.Runtime.withLock(func() {
		state.Runtime.Cooldowns[key] = cooldownUntil
		state.Runtime.Circuits[key] = &Circuit{
			State:               CircuitOpen,
			OpenUntil:           nowMs() + 30_000,
			ConsecutiveFailures: 2,
		}
	})

	snapshot := state.Runtime.Health(resolved, CurrentCircuitSettings())
	require.Len(t, snapshot.Members, 2)

	item := snapshot.Members[0]
	require.NotNil(t, item.CooldownUntil, "冷却中必须给出 cooldown_until")
	require.NotNil(t, item.CircuitOpenUntil, "熔断打开必须给出 circuit_open_until")

	parsedCooldown, err := time.Parse(time.RFC3339, *item.CooldownUntil)
	require.NoError(t, err, "cooldown_until 必须是 RFC3339")
	assert.Equal(t, cooldownUntil, parsedCooldown.UnixMilli(), "时间值不得漂移")
	assert.Equal(t, time.UTC, parsedCooldown.Location(), "必须是 UTC")

	parsedOpen, err := time.Parse(time.RFC3339, *item.CircuitOpenUntil)
	require.NoError(t, err)
	assert.Equal(t, clock.ms+30_000, parsedOpen.UnixMilli())

	// 未冷却/未打开的成员为 null，而不是 0 或空串。
	clean := snapshot.Members[1]
	assert.Nil(t, clean.CooldownUntil, "无冷却应为 null")
	assert.Nil(t, clean.CircuitOpenUntil, "熔断未打开应为 null")
}

// JSON 形状必须是字符串/null（不是数字），与 api-spec §6.5 示例一致。
func TestHealthSnapshotJSONShape(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-health-json", 1, 1)
	state := NewState(resolved)
	key := memberKeyOf(&resolved.Members[0])
	state.Runtime.withLock(func() {
		state.Runtime.Cooldowns[key] = nowMs() + 60_000
	})

	snapshot := state.Runtime.Health(resolved, CurrentCircuitSettings())
	payload, err := json.Marshal(snapshot)
	require.NoError(t, err)
	body := string(payload)

	assert.Contains(t, body, "\"cooldown_until\":\"20", "cooldown_until 必须是带引号的 RFC3339 字符串")
	assert.NotContains(t, body, "\"cooldown_until\":0", "不得再输出 unix 毫秒数字")
	assert.NotContains(t, body, "affinity_until", "affinity_until 必须已被 affinity 对象取代")
	assert.Contains(t, body, "\"affinity\":null", "无亲和时 affinity 为 null")
}

// 有亲和时 affinity 为对象，channel/upstream_model 来自当前成员，until 为 RFC3339。
func TestHealthSnapshotAffinityObject(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-health-affinity", 2, 1)
	state := NewState(resolved)

	member, _, ok := state.Next(nil)
	require.True(t, ok)
	require.Equal(t, "channel-a", memberName(member))
	state.Runtime.withLock(func() { state.Runtime.AffinityUntil = nowMs() + 300_000 })

	snapshot := state.Runtime.Health(resolved, CurrentCircuitSettings())
	require.NotNil(t, snapshot.Affinity, "亲和期内 affinity 必须是对象")
	assert.Equal(t, "channel-a", snapshot.Affinity.Channel)
	assert.Equal(t, "model-1", snapshot.Affinity.UpstreamModel)
	require.NotNil(t, snapshot.Affinity.Until)
	parsed, err := time.Parse(time.RFC3339, *snapshot.Affinity.Until)
	require.NoError(t, err)
	assert.Equal(t, nowMs()+300_000, parsed.UnixMilli())
}

// §2.1：manual 不参与冷却/亲和，快照必须按 mode 过滤残留冷却，展示与实际选路一致。
func TestHealthSnapshotManualFiltersCooldownAndAffinity(t *testing.T) {
	withFakeClock(t)
	resolved := testRoute("lane-health-manual", 2, 1)
	resolved.Mode = model.LaneModeManual
	resolved.ActiveMember = "channel-a/model-1"
	state := NewState(resolved)
	key := memberKeyOf(&resolved.Members[0])

	state.Runtime.withLock(func() {
		state.Runtime.Cooldowns[key] = nowMs() + 60_000
		state.Runtime.AffinityUntil = nowMs() + 300_000
		state.Runtime.HasCurrent = true
		state.Runtime.CurrentMember = key
	})

	snapshot := state.Runtime.Health(resolved, CurrentCircuitSettings())
	require.NotNil(t, snapshot)
	assert.Nil(t, snapshot.Affinity, "manual 不参与亲和：不得展示残留亲和")
	assert.Nil(t, snapshot.Members[0].CooldownUntil, "manual 不参与冷却：不得展示残留冷却")
	assert.True(t, snapshot.Members[0].Available, "manual 无熔断时恒可用（冷却不挡）")
	assert.True(t, strings.HasPrefix(snapshot.Members[0].Member, "channel-a"))
}
