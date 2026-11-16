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
import { api } from '@/lib/api'

// 渠道实时速度（GET /api/channel-speed，api-spec §6.14）。
// 口径与模型状态（§6.13）完全一致：p50/p95 只取成功请求（样本 <5 为 0），
// output_tps 只算干净样本（成功且单次尝试，流式扣掉首字等待）。返回全部配置
// 渠道，窗口内无请求的渠道以零值出现，界面显示 `—` 而不是 0。

// 默认 30s：输出速度是「该渠道窗口内全部 completion_tokens / 窗口秒数」，
// 窗口越长越会被空闲、排队与首字等待摊平；30s 更接近操作员实测的瞬时吐字速度。
export const CHANNEL_SPEED_WINDOWS = ['30s', '5m', '15m', '1h'] as const
export type ChannelSpeedWindow = (typeof CHANNEL_SPEED_WINDOWS)[number]

export const CHANNEL_SPEED_WINDOW_SECONDS: Record<ChannelSpeedWindow, number> =
  {
    '30s': 30,
    '5m': 5 * 60,
    '15m': 15 * 60,
    '1h': 60 * 60,
  }

export const DEFAULT_CHANNEL_SPEED_WINDOW: ChannelSpeedWindow = '30s'

export interface ChannelSpeedItem {
  channel: string
  enabled: boolean
  requests: number
  successes: number
  failures: number
  /** 0~1 小数；requests=0 时为 0（界面显示 `—`）。 */
  success_rate: number
  /** 成功请求耗时的中位（毫秒）；样本 <5 为 0（界面显示 `-`）。 */
  p50_total_ms: number
  /** 成功请求耗时的 95 分位（毫秒）；样本 <5 为 0。 */
  p95_total_ms: number
  /** 只对流式有样本；无样本为 0。 */
  avg_ttft_ms: number
  /** 干净样本的生成阶段速度（completion tokens / 生成耗时）；无样本为 0。 */
  output_tps: number
  /** 参与生成阶段速度折算的干净样本数。 */
  tps_samples: number
  /** 渠道整体输出速度：窗口内全部 completion tokens / 窗口秒数。 */
  output_tokens_per_second: number
  /** 生成阶段聚合速率：Σcompletion tokens / Σ生成耗时（并发合计吐字速度）。 */
  generation_tokens_per_second: number
  tokens_in: number
  tokens_out: number
  tokens_total: number
  estimated_cost: number
}

export interface ChannelSpeedResponse {
  from: number
  to: number
  items: ChannelSpeedItem[]
}

export async function getChannelSpeed(params: {
  from: number
  to: number
}): Promise<ChannelSpeedResponse> {
  const res = await api.get<ChannelSpeedResponse>('/api/channel-speed', {
    params,
  })
  return res.data
}
