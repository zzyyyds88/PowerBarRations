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
}

export type PBRStatsGroupBy =
  | 'lane'
  | 'channel'
  | 'key'
  | 'model'
  | 'channel_model'

// 「渠道 × 模型」分组的 group 形如 `渠道␟模型`（api-spec §5.5）。
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
