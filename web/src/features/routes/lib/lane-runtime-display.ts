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
/*
车道运行态展示纯逻辑（ui-spec §4）：成员级状态徽章集合与车道整体健康度。
不依赖 React（渲染在 components/lane-runtime-cell.tsx），便于独立单测。
*/
import type { StatusVariant } from '@/components/status-badge'
import dayjs from '@/lib/dayjs'
import type { LaneHealthSnapshot, LaneMemberHealth } from '@/lib/route-events'

/** 成员状态标识（内部口径）；渲染侧映射为 t() 字面量（web/AGENTS §3.1）。 */
export type MemberStateLabel =
  | 'Cooldown'
  | 'Circuit open'
  | 'Half-open'
  | 'Affinity'
  | 'Probing'
  | 'Current'
  | 'Manually disabled'

export interface MemberState {
  label: MemberStateLabel
  variant: StatusVariant
  pulse?: boolean
  /** 悬浮说明（截止时刻等）；空串表示无。 */
  detail: string
}

/** RFC3339 → 毫秒时间戳；null/非法值返回 NaN（调用方按"未到期"处理）。 */
function untilMs(value: string | null | undefined): number {
  if (!value) return Number.NaN
  const parsed = dayjs(value)
  return parsed.isValid() ? parsed.valueOf() : Number.NaN
}

function formatUntil(value: string | null | undefined): string {
  const ms = untilMs(value)
  return Number.isNaN(ms) ? '' : dayjs(ms).format('YYYY-MM-DD HH:mm:ss')
}

/**
 * 单个成员当前的运行态徽章（冷却/熔断/半开/亲和/探测占用/当前成员）。
 * `now` 由渲染层注入（组件渲染期禁调 Date.now，lint react(purity)）。
 */
export function laneMemberStates(
  snapshot: LaneHealthSnapshot,
  member: LaneMemberHealth,
  now: number
): MemberState[] {
  const states: MemberState[] = []
  // 人工关闭排在**最前**且用 neutral：它是配置态结论，与下面两个运行态故障徽章
  // （Cooldown=warning / Circuit open=danger）必须一眼可分——排障时"是运维关的"
  // 和"上游出故障了"是两种完全不同的结论（routing-spec §9、ui-spec §6.3）。
  // 被关闭成员的冷却/熔断字段仍照实给出，所以这里不短路：两类徽章可以并存。
  if (member.enabled === false) {
    states.push({ label: 'Manually disabled', variant: 'neutral', detail: '' })
  }
  if (member.circuit === 'open') {
    states.push({
      label: 'Circuit open',
      variant: 'danger',
      detail: formatUntil(member.circuit_open_until),
    })
  } else if (member.circuit === 'half-open') {
    states.push({ label: 'Half-open', variant: 'warning', detail: '' })
  }
  const cooldownMs = untilMs(member.cooldown_until)
  if (!Number.isNaN(cooldownMs) && cooldownMs > now) {
    states.push({
      label: 'Cooldown',
      variant: 'warning',
      detail: formatUntil(member.cooldown_until),
    })
  }
  const affinity = snapshot.affinity
  if (
    affinity &&
    affinity.channel === member.channel &&
    affinity.upstream_model === member.upstream_model
  ) {
    // until 为 null/缺失 = 无截止的持续亲和，同样展示徽章（不带时刻）。
    const affinityMs = untilMs(affinity.until)
    if (Number.isNaN(affinityMs) || affinityMs > now) {
      states.push({
        label: 'Affinity',
        variant: 'info',
        detail: formatUntil(affinity.until),
      })
    }
  }
  if (member.probing) {
    states.push({ label: 'Probing', variant: 'info', pulse: true, detail: '' })
  }
  if (member.current) {
    states.push({ label: 'Current', variant: 'success', detail: '' })
  }
  return states
}

/** 车道整体健康度：全部/部分/无可用成员 → healthy/degraded/unavailable。 */
export function laneRuntimeHealth(
  snapshot: LaneHealthSnapshot
): 'healthy' | 'degraded' | 'unavailable' {
  if (snapshot.members.length === 0) return 'healthy'
  const available = snapshot.members.filter((m) => m.available).length
  if (available === snapshot.members.length) return 'healthy'
  if (available === 0) return 'unavailable'
  return 'degraded'
}
