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

// PBR 聚合统计（GET /api/stats，api-spec §5.5）。
//
// 每个 bucket 是一个"时间粒度 × 分组维度"的聚合：请求数/成功数/token/折算成本。
// 成本按"渠道级上游单价 > 全局默认单价"折算（design-v1 §16.9#7）。

export type PBRStatBucket = {
  bucket_ts: number
  group: string
  requests: number
  successes: number
  prompt_tokens: number
  completion_tokens: number
  estimated_cost: number
  /** 归一输入总量（Anthropic 含 cache_read/write；OpenAI/Gemini 等于 prompt）。 */
  input_tokens?: number
  cache_read_tokens?: number
  cache_write_tokens?: number
  reasoning_tokens?: number
  ttft_ms_sum?: number
  ttft_samples?: number
  /** group_by=client 时后端给的展示字段：库/other → script。 */
  client_group?: string
  client_name?: string
}

export type PBRStatsGroupBy =
  | 'lane'
  | 'channel'
  | 'key'
  | 'model'
  | 'channel_model'
  | 'key_model'
  | 'client'

// 「渠道 × 模型」「令牌 × 模型」分组的 group 形如 `渠道␟模型` / `令牌␟模型`
// （api-spec §5.5）。**「模型」一律是 upstream_model（真实上游模型名，2026-10-25
// 起）**：车道名维度由 lane 单独承担，两个口径不混用。
export const PBR_CHANNEL_MODEL_SEPARATOR = '␟'

export type PBRStatsResponse = {
  granularity: 'hour' | 'day'
  group_by: PBRStatsGroupBy
  items: PBRStatBucket[]
}

export async function getPBRStats(params: {
  granularity: 'hour' | 'day'
  from: number
  to: number
  groupBy: PBRStatsGroupBy
}): Promise<PBRStatsResponse> {
  const res = await api.get<PBRStatsResponse>('/api/stats', {
    params: {
      granularity: params.granularity,
      from: params.from,
      to: params.to,
      group_by: params.groupBy,
    },
  })
  return res.data
}
