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
import { describe, expect, it } from 'vitest'

import type { LaneHealthSnapshot, LaneMemberHealth } from '@/lib/route-events'

import { laneMemberStates } from '../lane-runtime-display'

// ui-spec §6.3 / routing-spec §9：人工关闭是**配置态**结论，必须与运行态故障徽章
// （Cooldown / Circuit open）并列且颜色可区分，排障时能一眼分清"我关的"与"上游挂了"。

const NOW = 1_800_000_000_000
const FUTURE = new Date(NOW + 60_000).toISOString()

function member(extra?: Partial<LaneMemberHealth>): LaneMemberHealth {
  return {
    member: 'channel-a/model-1',
    channel: 'channel-a',
    upstream_model: 'model-1',
    circuit: 'closed',
    consecutive_failures: 0,
    cooldown_until: null,
    current: false,
    probing: false,
    available: true,
    ...extra,
  }
}

function snapshot(extra?: Partial<LaneHealthSnapshot>): LaneHealthSnapshot {
  return {
    lane: 'model-1',
    current_member: '',
    probe_member: '',
    affinity: null,
    members: [],
    ...extra,
  }
}

describe('laneMemberStates', () => {
  it('shows a neutral "Manually disabled" badge for enabled=false', () => {
    const states = laneMemberStates(
      snapshot(),
      member({ enabled: false, available: false }),
      NOW
    )
    const disabled = states.find((s) => s.label === 'Manually disabled')
    expect(disabled).toBeDefined()
    // neutral 是与 Cooldown(warning)/Circuit open(danger) 区分开的关键。
    expect(disabled?.variant).toBe('neutral')
  })

  it('does not show the badge when enabled is missing (old backend) or true', () => {
    for (const enabled of [undefined, true]) {
      const states = laneMemberStates(snapshot(), member({ enabled }), NOW)
      expect(
        states.find((s) => s.label === 'Manually disabled')
      ).toBeUndefined()
    }
  })

  it('keeps the disabled badge side by side with runtime badges', () => {
    // 被关闭成员的运行态字段仍照实给出（routing-spec §7），所以两类徽章必须并存，
    // 而不是"关闭"把冷却/熔断盖掉——否则排障看不到关闭前的状态。
    const states = laneMemberStates(
      snapshot(),
      member({
        enabled: false,
        available: false,
        circuit: 'open',
        cooldown_until: FUTURE,
        consecutive_failures: 3,
      }),
      NOW
    )
    const labels = states.map((s) => s.label)
    expect(labels).toContain('Manually disabled')
    expect(labels).toContain('Circuit open')
    expect(labels).toContain('Cooldown')

    const variantOf = (label: string) =>
      states.find((s) => s.label === label)?.variant
    expect(variantOf('Manually disabled')).toBe('neutral')
    expect(variantOf('Circuit open')).toBe('danger')
    expect(variantOf('Cooldown')).toBe('warning')
    // 三者 variant 两两不同 = 肉眼可区分。
    expect(
      new Set([
        variantOf('Manually disabled'),
        variantOf('Circuit open'),
        variantOf('Cooldown'),
      ]).size
    ).toBe(3)
  })
})
