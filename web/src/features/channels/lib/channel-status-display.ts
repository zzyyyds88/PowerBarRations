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
渠道状态详情的纯展示逻辑（不依赖 React，便于单测；渲染在
components/drawers/channel-status-*.tsx）。

口径与车道运行态展示（features/routes/lib/lane-runtime-display.ts）保持同义：
- 冷却只在 `cooldown_until > now` 时算"冷却中"，过期即按未冷却展示；
- 熔断态 closed/half-open/open 的颜色语言与车道一致（warning/danger），
  人工关闭用 neutral 灰（它出现在成员列，不冒充故障）。
`now` 一律由数据层注入（渲染期禁调 Date.now，lint react(purity)）。
*/
import type { StatusVariant } from '@/components/status-badge'
import { formatUptimePct } from '@/features/performance-metrics/lib/format'
import dayjs from '@/lib/dayjs'

import { CHANNEL_PROTOCOL_PRESENTATION } from '../constants'
import type { Channel } from '../types'
import {
  getChannelProtocolFromChannel,
  getChannelProtocolLabelKey,
} from './channel-utils'

/** 熔断三态 → 展示变体；未知取值按中性展示，不臆造状态。 */
export function circuitVariant(circuit: string): StatusVariant {
  if (circuit === 'open') return 'danger'
  if (circuit === 'half-open') return 'warning'
  if (circuit === 'closed') return 'success'
  return 'neutral'
}

/** RFC3339 → 毫秒时间戳；null/非法值返回 NaN（调用方按"未到期"处理）。 */
function timestampMs(value: string | null | undefined): number {
  if (!value) return Number.NaN
  const parsed = dayjs(value)
  return parsed.isValid() ? parsed.valueOf() : Number.NaN
}

/** 冷却是否仍在生效（过期或未设置都算不在冷却）。 */
export function isCooldownActive(
  cooldownUntil: string | null | undefined,
  now: number
): boolean {
  const ms = timestampMs(cooldownUntil)
  return !Number.isNaN(ms) && ms > now
}

/** RFC3339 → `YYYY-MM-DD HH:mm:ss`；空/非法值返回空串（调用方渲染占位符）。 */
export function formatStatusTime(value: string | null | undefined): string {
  const ms = timestampMs(value)
  return Number.isNaN(ms) ? '' : dayjs(ms).format('YYYY-MM-DD HH:mm:ss')
}

/** 逐日桶的短标签（`2026-10-04` → `10-04`）；非法值原样返回。 */
export function formatDailyLabel(date: string): string {
  const parsed = dayjs(date)
  return parsed.isValid() ? parsed.format('MM-DD') : date
}

/** 成功率（0~1 小数）→ 百分比字符串；非有限值返回破折号。 */
export function formatSuccessRate(rate: number | null | undefined): string {
  if (typeof rate !== 'number' || !Number.isFinite(rate)) return '—'
  return formatUptimePct(rate * 100)
}

/** 毫秒耗时 → 可读字符串（0/非法值按"未取到"处理）。 */
export function formatDurationMs(
  durationMs: number | null | undefined
): string {
  if (
    typeof durationMs !== 'number' ||
    !Number.isFinite(durationMs) ||
    durationMs <= 0
  ) {
    return '-'
  }
  if (durationMs >= 1000) return `${(durationMs / 1000).toFixed(2)}s`
  return `${Math.round(durationMs)}ms`
}

/**
 * 是否出现过 `upstream_reject`（routing-spec §4.1）：它只说明"该成员拒绝过这份
 * 请求"，不计冷却、不计熔断分，必须与真实故障区分开（api-spec §6.5/§6.12）。
 */
export function hasUpstreamReject(
  values: Array<string | null | undefined>
): boolean {
  return values.some((value) => value === 'upstream_reject')
}

/**
 * 输出速度（tokens/s，已排除首字等待）：无样本（0/非法）显示 `-`。
 * 不显示 0 是有意的——"没有样本"与"吐字速度为 0"是两回事（api-spec §6.12）。
 */
export function formatOutputTps(tps: number | null | undefined): string {
  if (typeof tps !== 'number' || !Number.isFinite(tps) || tps <= 0) return '-'
  return tps.toFixed(1)
}

/**
 * 模型行的成功率：窗口内没有请求（无样本）时显示 `-`，而不是拿 0% 冒充"全失败"。
 */
export function formatModelSuccessRate(stat: {
  requests: number
  success_rate: number
}): string {
  if (!Number.isFinite(stat.requests) || stat.requests <= 0) return '-'
  return formatSuccessRate(stat.success_rate)
}

/**
 * 协议展示键（列表「协议」列、抽屉协议徽章共用）：
 * 控制台渠道记录的渠道级协议优先（它比适配器名更精确），记录缺失时按契约返回的
 * 适配器名（`GET /api/channels/status` 的 `type`，如 `openai`）兜底；
 * 两者都给不出时返回 null，调用方按"未知"处理。
 */
export function resolveProtocolLabelKey(
  rowChannel: Channel | null | undefined,
  adapterType: string | null | undefined
): string | null {
  if (rowChannel) {
    return getChannelProtocolLabelKey(getChannelProtocolFromChannel(rowChannel))
  }
  if (!adapterType) return null
  const normalized = adapterType.trim().toLowerCase()
  if (
    normalized === 'openai' ||
    normalized === 'openai-chat' ||
    normalized === 'openai-compatible' ||
    normalized === 'openai_compatible'
  ) {
    return CHANNEL_PROTOCOL_PRESENTATION['openai-chat'].shortLabel
  }
  if (normalized === 'openai-responses' || normalized === 'openai_responses') {
    return CHANNEL_PROTOCOL_PRESENTATION['openai-responses'].shortLabel
  }
  if (normalized === 'anthropic' || normalized === 'claude') {
    return CHANNEL_PROTOCOL_PRESENTATION.anthropic.shortLabel
  }
  if (normalized === 'gemini' || normalized === 'google') {
    return CHANNEL_PROTOCOL_PRESENTATION.gemini.shortLabel
  }
  return null
}
