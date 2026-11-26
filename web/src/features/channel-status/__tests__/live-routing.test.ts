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
import dayjs from 'dayjs'
import { describe, expect, it } from 'vitest'

import type { PBRStatBucket } from '@/features/dashboard/pbr-stats-api'
import type { PBRLaneSummary } from '@/features/routes/api'
import type { LaneHealthSnapshot, LaneMemberHealth } from '@/lib/route-events'

import {
  buildMemberRows,
  firstAvailableMember,
  laneAvailability,
  laneMemberCount,
  laneStatsMap,
  laneWindowStats,
  memberLabel,
  pickDefaultLane,
  remainingParts,
  resolveLanePolicy,
  resolveMemberLiveState,
  secondsUntil,
  sortLanesForRail,
} from '../lib/live-routing'

const NOW = dayjs('2026-11-05T10:00:00Z').valueOf()
const inSeconds = (seconds: number) =>
  dayjs(NOW).add(seconds, 'second').toISOString()

function laneMember(
  overrides: Partial<LaneMemberHealth> = {}
): LaneMemberHealth {
  return {
    member: 'alpha/model',
    channel: 'alpha',
    upstream_model: 'model',
    circuit: 'closed',
    consecutive_failures: 0,
    failure_score: 0,
    rolling_success_rate: 0,
    cooldown_until: null,
    circuit_open_until: null,
    last_error_kind: '',
    current: false,
    probing: false,
    available: true,
    enabled: true,
    ...overrides,
  }
}

function snapshot(
  members: LaneMemberHealth[],
  overrides: Partial<LaneHealthSnapshot> = {}
): LaneHealthSnapshot {
  return {
    lane: 'lane-a',
    current_member: '',
    probe_member: '',
    affinity: null,
    members,
    ...overrides,
  }
}

function lane(overrides: Partial<PBRLaneSummary> = {}): PBRLaneSummary {
  return {
    name: 'lane-a',
    enabled: true,
    mode: 'failover',
    active_member: '',
    orphan_member_count: 0,
    members: [
      {
        channel: 'alpha',
        model: 'model',
        upstream_model: 'model',
        priority: 10,
        enabled: true,
        channel_enabled: true,
      },
      {
        channel: 'beta',
        model: 'model',
        upstream_model: 'model',
        priority: 5,
        enabled: true,
        channel_enabled: true,
      },
    ],
    ...overrides,
  }
}

function bucket(
  group: string,
  requests: number,
  successes: number
): PBRStatBucket {
  return {
    bucket_ts: 1790000000,
    group,
    requests,
    successes,
    prompt_tokens: 0,
    completion_tokens: 0,
    estimated_cost: 0,
  }
}

describe('secondsUntil', () => {
  it('rounds up and ignores expired/invalid values', () => {
    expect(secondsUntil(inSeconds(42), NOW)).toBe(42)
    expect(secondsUntil(inSeconds(1.2), NOW)).toBe(2)
    expect(secondsUntil(inSeconds(-1), NOW)).toBeNull()
    expect(secondsUntil('not-a-date', NOW)).toBeNull()
    expect(secondsUntil(null, NOW)).toBeNull()
  })
})

describe('resolveMemberLiveState', () => {
  it('treats lane/member/channel switches before runtime failures', () => {
    expect(
      resolveMemberLiveState({ laneEnabled: false, available: false }, NOW).kind
    ).toBe('lane-disabled')
    expect(
      resolveMemberLiveState({ enabled: false, available: false }, NOW).kind
    ).toBe('disabled')
    expect(
      resolveMemberLiveState({ channelEnabled: false, available: false }, NOW)
        .kind
    ).toBe('channel-disabled')
  })

  it('distinguishes rate limits from generic cooldowns', () => {
    const rateLimited = resolveMemberLiveState(
      {
        available: true,
        pacedUntil: inSeconds(30),
        lastErrorKind: 'soft_rate_limit',
      },
      NOW
    )
    expect(rateLimited.kind).toBe('rate-limited')
    expect(rateLimited.secondsLeft).toBe(30)

    const cooldown = resolveMemberLiveState(
      { cooldownUntil: inSeconds(60), lastErrorKind: 'soft_transient' },
      NOW
    )
    expect(cooldown.kind).toBe('cooldown')
    expect(cooldown.secondsLeft).toBe(60)
  })

  it('keeps current flag while the member is cooling down', () => {
    const state = resolveMemberLiveState(
      { current: true, cooldownUntil: inSeconds(10) },
      NOW
    )
    expect(state.kind).toBe('cooldown')
    expect(state.current).toBe(true)
  })

  it('prefers circuit-open over cooldown and accepts both half-open spellings', () => {
    const open = resolveMemberLiveState(
      {
        circuit: 'open',
        circuitOpenUntil: inSeconds(120),
        cooldownUntil: inSeconds(5),
      },
      NOW
    )
    expect(open.kind).toBe('circuit-open')
    expect(open.secondsLeft).toBe(120)
    expect(resolveMemberLiveState({ circuit: 'half_open' }, NOW).kind).toBe(
      'half-open'
    )
    expect(resolveMemberLiveState({ circuit: 'half-open' }, NOW).kind).toBe(
      'half-open'
    )
  })

  it('falls through probing, current, ready and unavailable', () => {
    expect(resolveMemberLiveState({ probing: true }, NOW).kind).toBe('probing')
    expect(resolveMemberLiveState({ probing: true }, NOW).pulse).toBe(true)
    expect(
      resolveMemberLiveState({ current: true, available: true }, NOW).kind
    ).toBe('current')
    expect(resolveMemberLiveState({ available: true }, NOW).kind).toBe('ready')
    expect(resolveMemberLiveState({ available: false }, NOW).kind).toBe(
      'unavailable'
    )
    // 当前成员但已不可用：行仍可高亮为当前，文字必须是“不可用”。
    const staleCurrent = resolveMemberLiveState(
      { current: true, available: false },
      NOW
    )
    expect(staleCurrent.kind).toBe('unavailable')
    expect(staleCurrent.current).toBe(true)
  })

  it('surfaces a member that keeps getting rejected as "rejected"', () => {
    // upstream_reject 不计冷却、不写熔断，所以 available 仍为 true。若不单独
    // 标记，界面会显示「就绪」，而真实行为是每次请求都先白打它一次。
    // 文案只陈述连续拒绝次数，不推断配置过错（内容审查/参数不支持也是 400）。
    const state = resolveMemberLiveState(
      {
        available: true,
        lastErrorKind: 'upstream_reject',
        rejectStreak: 3,
      },
      NOW
    )
    expect(state.kind).toBe('rejected')
    expect(state.streak).toBe(3)
  })

  it('keeps cooldown wording when a rejected member is resting', () => {
    const state = resolveMemberLiveState(
      {
        available: false,
        lastErrorKind: 'upstream_reject',
        rejectStreak: 5,
        cooldownUntil: inSeconds(120),
      },
      NOW
    )
    expect(state.kind).toBe('cooldown')
    expect(state.secondsLeft).toBe(120)
  })

  it('does not mark a member rejected without a streak', () => {
    expect(
      resolveMemberLiveState(
        { available: true, lastErrorKind: 'upstream_reject' },
        NOW
      ).kind
    ).toBe('ready')
  })
})

describe('laneAvailability', () => {
  it('counts live availability from the snapshot', () => {
    const live = snapshot([
      laneMember(),
      laneMember({ member: 'beta/model', channel: 'beta', available: false }),
    ])
    expect(laneAvailability(lane(), live)).toEqual({
      available: 1,
      total: 2,
      health: 'degraded',
    })
  })

  it('reports disabled and waiting states without pretending live data', () => {
    expect(laneAvailability(lane({ enabled: false }))).toEqual({
      available: 0,
      total: 2,
      health: 'disabled',
    })
    expect(laneAvailability(lane()).health).toBe('waiting')
  })

  it('uses only the named member as the manual lane denominator', () => {
    // manual 只有 active_member 参与选路：其他成员 available=false 是设计使然，
    // 不能把整条车道渲染成 degraded（routing-spec §2.1/§7）。
    const live = snapshot([
      laneMember(),
      laneMember({ member: 'beta/model', channel: 'beta', available: false }),
    ])
    expect(laneAvailability(lane({ mode: 'manual' }), live)).toEqual({
      available: 1,
      total: 1,
      health: 'healthy',
    })
  })

  it('counts group references by their member count', () => {
    const grouped = lane({
      members: [
        {
          channel: '',
          model: '',
          upstream_model: '',
          group: 'group-a',
          group_member_count: 3,
          enabled: true,
          group_enabled: true,
        },
      ],
    })
    expect(laneMemberCount(grouped)).toBe(3)
    expect(laneAvailability(grouped)).toEqual({
      available: 3,
      total: 3,
      health: 'waiting',
    })
  })
})

describe('member rows and first available', () => {
  it('uses snapshot order and the channel-enabled map', () => {
    const live = snapshot([
      laneMember({ member: 'alpha/model' }),
      laneMember({
        member: 'beta/model',
        channel: 'beta',
        available: false,
        enabled: false,
      }),
    ])
    const rows = buildMemberRows({ lane: lane(), snapshot: live, now: NOW })
    expect(rows.map((row) => row.channel)).toEqual(['alpha', 'beta'])
    expect(rows[0].state.kind).toBe('ready')
    expect(rows[1].state.kind).toBe('disabled')
    expect(firstAvailableMember(live)?.channel).toBe('alpha')
    expect(memberLabel(live.members[0])).toBe('alpha/model')
  })

  it('renders waiting rows from summary while runtime is cold', () => {
    const rows = buildMemberRows({ lane: lane(), now: NOW })
    expect(rows).toHaveLength(2)
    expect(rows.every((row) => row.state.kind === 'waiting')).toBe(true)
  })

  it('skips disabled members when choosing the first available', () => {
    const live = snapshot([
      laneMember({ enabled: false, available: false }),
      laneMember({ member: 'beta/model', channel: 'beta' }),
    ])
    expect(firstAvailableMember(live)?.channel).toBe('beta')
  })
})

describe('lane policy and stats', () => {
  it('falls back to built-in policy values and normalizes the mode', () => {
    expect(resolveLanePolicy(undefined, null)).toEqual({
      mode: 'failover',
      cooldownSeconds: 60,
      maxAttempts: 2,
      affinitySeconds: 0,
    })
    expect(
      resolveLanePolicy('manual', {
        member_cooldown_seconds: 90,
        member_max_attempts: 3,
        member_affinity_seconds: 30,
      })
    ).toEqual({
      mode: 'manual',
      cooldownSeconds: 90,
      maxAttempts: 3,
      affinitySeconds: 30,
    })
  })

  it('aggregates lane buckets and ignores other groups', () => {
    const buckets = [
      bucket('lane-a', 3, 3),
      bucket('lane-a', 2, 1),
      bucket('lane-b', 100, 100),
    ]
    expect(laneWindowStats(buckets, 'lane-a')).toEqual({
      requests: 5,
      failures: 1,
      successRate: 0.8,
    })
    expect(laneStatsMap(buckets).get('lane-b')?.requests).toBe(100)
  })

  it('splits remaining seconds for i18n composition', () => {
    expect(remainingParts(3661)).toEqual({
      hours: 1,
      minutes: 1,
      seconds: 1,
    })
  })
})

describe('lane defaults', () => {
  it('keeps enabled lanes first without reordering within groups', () => {
    const lanes = [
      lane({ name: 'off', enabled: false }),
      lane({ name: 'first', enabled: true }),
      lane({ name: 'second', enabled: true }),
    ]
    expect(sortLanesForRail(lanes).map((item) => item.name)).toEqual([
      'first',
      'second',
      'off',
    ])
  })

  it('picks the busiest enabled lane, falling back to the first one', () => {
    const lanes = [
      lane({ name: 'alpha' }),
      lane({ name: 'beta' }),
      lane({ name: 'off', enabled: false }),
    ]
    const stats = new Map([
      ['alpha', { requests: 1, failures: 0, successRate: 1 }],
      ['beta', { requests: 9, failures: 0, successRate: 1 }],
      ['off', { requests: 100, failures: 0, successRate: 1 }],
    ])
    expect(pickDefaultLane(lanes, stats)).toBe('beta')
    expect(pickDefaultLane(lanes, new Map())).toBe('alpha')
    expect(pickDefaultLane([], new Map())).toBeUndefined()
  })
})
