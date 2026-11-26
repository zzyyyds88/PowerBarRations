/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { PBRStatBucket } from '@/features/dashboard/pbr-stats-api'
import type { PBRLaneSummary } from '@/features/routes/api'
/*
渠道状态页「实时路由」的纯逻辑（不依赖 React，便于单测）。

数据口径（routing-spec §5/§6、api-spec §5.7、§6.5）：
- 成员运行态来自 SSE / 健康轮询快照（`LaneMemberHealth`），不是页面自己猜的；
- 成员顺序以快照为准（后端解析成员组后按优先级降序给出）；
- 冷却/熔断倒计时用 `now` 注入，渲染期不调 `Date.now()`（react/purity）。
*/
import dayjs from '@/lib/dayjs'
import type { LaneHealthSnapshot, LaneMemberHealth } from '@/lib/route-events'

/** 成员实时状态。`current` 与故障态可以并存（当前成员也可能正在冷却）。 */
export type MemberLiveKind =
  | 'lane-disabled'
  | 'disabled'
  | 'channel-disabled'
  | 'circuit-open'
  | 'rate-limited'
  | 'cooldown'
  | 'probing'
  | 'half-open'
  /**
   * 连续被上游拒绝（upstream_reject）且仍可被选路。
   *
   * 这是事实陈述，不是配置结论：拒绝可能是内容审查/参数不支持（只针对这一份
   * 请求），也可能是网关没转发上游必需的头（配置问题）。界面只显示「上游拒绝
   * N 次」，是否要改配置由运维结合上游报文判断。
   */
  | 'rejected'
  | 'current'
  | 'ready'
  | 'unavailable'
  | 'waiting'

export interface MemberLiveState {
  kind: MemberLiveKind
  /** 是否为车道当前成员（亲和/粘性选中的成员）。 */
  current: boolean
  /** 冷却/熔断剩余秒数；无截止时间或已到期为 null。 */
  secondsLeft: number | null
  /** 需要脉冲提示（探活占用中）。 */
  pulse: boolean
  /** 连续上游拒绝次数（仅 `rejected` 状态有值；后端恒回）。 */
  streak?: number
}

/** 成员实时状态的输入：与 `LaneMemberHealth` 同形，另加车道/渠道开关。 */
export interface MemberLiveInput {
  enabled?: boolean
  available?: boolean
  circuit?: string
  cooldownUntil?: string | null
  /** 限流减速截止（429 语义；与冷却分开，不影响 available）。 */
  pacedUntil?: string | null
  circuitOpenUntil?: string | null
  probing?: boolean
  current?: boolean
  lastErrorKind?: string
  /** 连续 upstream_reject 次数：>0 说明它在白打，配置大概率有问题。 */
  rejectStreak?: number
  laneEnabled?: boolean
  channelEnabled?: boolean
}

/** RFC3339 → 距 now 的剩余秒数（向上取整）。无效/已到期返回 null。 */
export function secondsUntil(
  value: string | null | undefined,
  now: number
): number | null {
  if (!value) return null
  const target = dayjs(value)
  if (!target.isValid()) return null
  const diff = target.valueOf() - now
  if (!Number.isFinite(diff) || diff <= 0) return null
  return Math.ceil(diff / 1000)
}

/**
 * 单个成员的实时状态（显示优先级）：
 * 车道停用 > 成员停用 > 渠道停用 > 熔断打开 > 冷却/限流 > 探活中 > 半开 > 当前 > 就绪 > 不可用。
 *
 * 熔断打开优先于冷却：熔断是更重的结论，且两者的恢复时间可能不同。
 * `current` 单独保留，即使成员正在冷却，行仍可标记为“当前成员”。
 */
export function resolveMemberLiveState(
  input: MemberLiveInput,
  now: number
): MemberLiveState {
  const current = Boolean(input.current)
  const base = { current, secondsLeft: null, pulse: false } as const

  if (input.laneEnabled === false) return { ...base, kind: 'lane-disabled' }
  if (input.enabled === false) return { ...base, kind: 'disabled' }
  if (input.channelEnabled === false) {
    return { ...base, kind: 'channel-disabled' }
  }
  if (input.circuit === 'open') {
    return {
      ...base,
      kind: 'circuit-open',
      secondsLeft: secondsUntil(input.circuitOpenUntil, now),
    }
  }

  const cooldownLeft = secondsUntil(input.cooldownUntil, now)
  if (cooldownLeft !== null) {
    return { ...base, kind: 'cooldown', secondsLeft: cooldownLeft }
  }

  // 429 只减速（routing-spec §4.1.1）：成员仍 available，不是冷却/故障。
  // 单独成『限流中』，排障时不会误判成上游挂了。
  const pacedLeft = secondsUntil(input.pacedUntil, now)
  if (pacedLeft !== null) {
    return { ...base, kind: 'rate-limited', secondsLeft: pacedLeft }
  }

  // 连续被上游拒绝：upstream_reject 不计熔断、不写冷却，所以成员仍「就绪」，
  // 但每次请求都会先白打它一次。这里只陈述事实（连续 N 次被拒），不推断配置
  // 过错——内容审查/参数不支持同样会以 400 到达。冷却中的拒绝由上面的 cooldown
  // 分支表达，不重复。
  if (
    (input.rejectStreak ?? 0) > 0 &&
    input.lastErrorKind === 'upstream_reject'
  ) {
    return { ...base, kind: 'rejected', streak: input.rejectStreak }
  }

  if (input.probing) return { ...base, kind: 'probing', pulse: true }
  // 后端常量是 `half_open`；历史注释/前端曾写 `half-open`，两种都认。
  if (input.circuit === 'half-open' || input.circuit === 'half_open') {
    return { ...base, kind: 'half-open' }
  }
  // “当前成员”只在真的可选时成立：当前成员若因未知原因不可用（例如渠道在
  // 组内被停用、摘要里拿不到 channel_enabled），状态文字必须说“不可用”，
  // 不能拿“当前”掩盖故障。
  if (current && input.available) {
    return { ...base, kind: 'current', current: true }
  }
  if (input.available) return { ...base, kind: 'ready' }
  return { ...base, kind: 'unavailable' }
}

/** 把快照成员映射成状态输入（渠道开关来自车道摘要，组内成员可能缺席）。 */
export function memberStateInput(
  member: LaneMemberHealth,
  options: {
    laneEnabled?: boolean
    channelEnabled?: boolean
  } = {}
): MemberLiveInput {
  return {
    enabled: member.enabled,
    available: member.available,
    circuit: member.circuit,
    cooldownUntil: member.cooldown_until,
    pacedUntil: member.paced_until,
    circuitOpenUntil: member.circuit_open_until,
    probing: member.probing,
    current: member.current,
    lastErrorKind: member.last_error_kind,
    rejectStreak: member.reject_streak,
    laneEnabled: options.laneEnabled,
    channelEnabled: options.channelEnabled,
  }
}

/** 车道可用度（车道卡片用）。无快照时返回 `waiting`，不把配置态冒充运行态。 */
export type LaneHealth =
  | 'healthy'
  | 'degraded'
  | 'unavailable'
  | 'disabled'
  | 'waiting'

export interface LaneAvailability {
  available: number
  total: number
  health: LaneHealth
}

/** 车道成员数：组引用按组内成员数计，散成员计 1。 */
export function laneMemberCount(lane: PBRLaneSummary): number {
  return lane.members.reduce(
    (sum, member) =>
      sum + (member.group ? Math.max(1, member.group_member_count ?? 1) : 1),
    0
  )
}

/** 车道可用度：优先用运行态快照；没有快照时只报配置态可用的估算值。 */
export function laneAvailability(
  lane: PBRLaneSummary,
  snapshot?: LaneHealthSnapshot
): LaneAvailability {
  const total = snapshot?.members.length
    ? snapshot.members.length
    : laneMemberCount(lane)
  if (lane.enabled === false) {
    return { available: 0, total, health: 'disabled' }
  }
  if (!snapshot) {
    const configured = lane.members.reduce((sum, member) => {
      if (member.group) {
        const off = member.enabled === false || member.group_enabled === false
        return sum + (off ? 0 : Math.max(1, member.group_member_count ?? 1))
      }
      const off = member.enabled === false || member.channel_enabled === false
      return sum + (off ? 0 : 1)
    }, 0)
    return { available: configured, total, health: 'waiting' }
  }

  const available = snapshot.members.filter(
    (member) => member.enabled !== false && member.available
  ).length
  if (lane.mode === 'manual') {
    // manual 只有点名成员参与选路：非点名成员 available=false 是设计使然，
    // 不能算进 degraded 分母（否则每条 manual 车道永远显示"部分可用"）。
    return available > 0
      ? { available: 1, total: 1, health: 'healthy' }
      : { available: 0, total: 1, health: 'unavailable' }
  }
  if (total === 0) return { available: 0, total, health: 'healthy' }
  if (available === 0) return { available, total, health: 'unavailable' }
  if (available < total) return { available, total, health: 'degraded' }
  return { available, total, health: 'healthy' }
}

/** 当前首选（failover 下第一个可用成员；manual 下 available 只对点名成员为真）。 */
export function firstAvailableMember(
  snapshot: LaneHealthSnapshot | undefined
): LaneMemberHealth | null {
  if (!snapshot) return null
  return (
    snapshot.members.find(
      (member) => member.enabled !== false && member.available
    ) ?? null
  )
}

/** 成员展示标签（渠道/上游真名），与后端 `memberLabel` 同形。 */
export function memberLabel(member: {
  channel: string
  upstream_model: string
}): string {
  return `${member.channel}/${member.upstream_model}`
}

/** 车道卡片排序：启用的在前，其余保持后端创建顺序（stable sort）。 */
export function sortLanesForRail(lanes: PBRLaneSummary[]): PBRLaneSummary[] {
  return [...lanes].sort(
    (left, right) =>
      Number(right.enabled !== false) - Number(left.enabled !== false)
  )
}

/** 默认选中车道：启用车道里窗口请求数最高者；全为 0 时取第一条启用车道。 */
export function pickDefaultLane(
  lanes: PBRLaneSummary[],
  statsByLane: Map<string, LaneWindowStats>
): string | undefined {
  const enabled = lanes.filter((lane) => lane.enabled !== false)
  const pool = enabled.length > 0 ? enabled : lanes
  let best: { name: string; requests: number } | null = null
  for (const lane of pool) {
    const requests = statsByLane.get(lane.name)?.requests ?? 0
    if (!best || requests > best.requests) {
      best = { name: lane.name, requests }
    }
  }
  return best?.name
}

/** 车道策略六键（缺省回落内置默认值；与后端 `BuiltinLaneRelayConfig` 一致）。 */
export interface LanePolicy {
  mode: 'failover' | 'manual'
  cooldownSeconds: number
  maxAttempts: number
  affinitySeconds: number
}

export const LANE_POLICY_DEFAULTS = {
  cooldownSeconds: 60,
  maxAttempts: 2,
  affinitySeconds: 0,
} as const

function positiveNumber(raw: number | undefined, fallback: number): number {
  return typeof raw === 'number' && Number.isFinite(raw) && raw >= 0
    ? raw
    : fallback
}

export function resolveLanePolicy(
  mode: string | undefined,
  config: Record<string, number> | null | undefined
): LanePolicy {
  return {
    mode: mode === 'manual' ? 'manual' : 'failover',
    cooldownSeconds: positiveNumber(
      config?.member_cooldown_seconds,
      LANE_POLICY_DEFAULTS.cooldownSeconds
    ),
    maxAttempts: positiveNumber(
      config?.member_max_attempts,
      LANE_POLICY_DEFAULTS.maxAttempts
    ),
    affinitySeconds: positiveNumber(
      config?.member_affinity_seconds,
      LANE_POLICY_DEFAULTS.affinitySeconds
    ),
  }
}

/** 把剩余秒数拆成时/分/秒，供 i18n 文案组合。 */
export function remainingParts(totalSeconds: number): {
  hours: number
  minutes: number
  seconds: number
} {
  const safe = Math.max(0, Math.floor(totalSeconds))
  return {
    hours: Math.floor(safe / 3600),
    minutes: Math.floor((safe % 3600) / 60),
    seconds: safe % 60,
  }
}

/** 车道窗口统计（从 /api/stats?group_by=lane 的小时桶汇总）。 */
export interface LaneWindowStats {
  requests: number
  failures: number
  successRate: number | null
}

export function laneWindowStats(
  buckets: PBRStatBucket[],
  lane: string
): LaneWindowStats {
  return (
    laneStatsMap(buckets).get(lane) ?? {
      requests: 0,
      failures: 0,
      successRate: null,
    }
  )
}

export function laneStatsMap(
  buckets: PBRStatBucket[]
): Map<string, LaneWindowStats> {
  const raw = new Map<string, { requests: number; successes: number }>()
  for (const bucket of buckets) {
    const current = raw.get(bucket.group) ?? { requests: 0, successes: 0 }
    current.requests += bucket.requests
    current.successes += bucket.successes
    raw.set(bucket.group, current)
  }
  const map = new Map<string, LaneWindowStats>()
  for (const [group, counts] of raw) {
    map.set(group, {
      requests: counts.requests,
      failures: Math.max(0, counts.requests - counts.successes),
      successRate:
        counts.requests > 0 ? counts.successes / counts.requests : null,
    })
  }
  return map
}

/** 实时路由右栏的一行：快照优先；无快照时用车道摘要（组引用不展开）。 */
export interface LiveMemberRow {
  key: string
  channel: string
  upstreamModel: string
  group?: string
  groupMemberCount?: number
  state: MemberLiveState
}

export function buildMemberRows(params: {
  lane: PBRLaneSummary
  snapshot?: LaneHealthSnapshot
  now: number
}): LiveMemberRow[] {
  const channelEnabled = new Map<string, boolean>()
  for (const member of params.lane.members) {
    if (!member.group && member.channel) {
      channelEnabled.set(
        `${member.channel}\u0000${member.upstream_model}`,
        member.channel_enabled !== false
      )
    }
  }

  if (params.snapshot) {
    return params.snapshot.members.map((member) => ({
      key: `${member.member}\u0000${member.upstream_model}`,
      channel: member.channel,
      upstreamModel: member.upstream_model,
      state: resolveMemberLiveState(
        memberStateInput(member, {
          laneEnabled: params.lane.enabled,
          channelEnabled: channelEnabled.get(
            `${member.channel}\u0000${member.upstream_model}`
          ),
        }),
        params.now
      ),
    }))
  }

  return params.lane.members.map((member) => {
    if (member.group) {
      return {
        key: `group:${member.group}:${member.priority ?? ''}`,
        channel: member.group,
        upstreamModel: '',
        group: member.group,
        groupMemberCount: member.group_member_count,
        state: {
          kind: 'waiting' as const,
          current: false,
          secondsLeft: null,
          pulse: false,
        },
      }
    }
    return {
      key: `${member.channel}\u0000${member.upstream_model}`,
      channel: member.channel,
      upstreamModel: member.upstream_model,
      state: {
        kind: 'waiting' as const,
        current: false,
        secondsLeft: null,
        pulse: false,
      },
    }
  })
}
