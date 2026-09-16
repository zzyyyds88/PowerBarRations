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

/** GET /api/logs 的元素（api-spec §5.5 的元数据口径）。 */
export interface PBRRequestLogListItem {
  id: number
  ts: string
  lane: string
  request_model: string
  route_source: string
  channel: string
  upstream_model: string
  key_name: string
  inbound_format: string
  success: boolean
  http_status: number
  error_kind: string
  error_summary: string
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  reasoning_tokens: number
  ttft_ms: number
  total_ms: number
  is_stream: boolean
  attempts: PBRAttempt[]
  total_attempts: number
  estimated_cost: number
}

/** 一次尝试（routing-spec §9）。 */
export interface PBRAttempt {
  attempt_num: number
  member: string
  status: string
  duration_ms: number
  error_kind?: string
  msg?: string
}

export interface PBRLogFilters {
  lane?: string
  channel?: string
  key?: string
  model?: string
  success?: boolean
  limit?: number
  cursor?: string
}

interface PBRLogListResponse {
  items: PBRRequestLogListItem[]
  next_cursor: string | null
}

/** GET /api/logs：PBR 元数据日志（含 attempts 链）。 */
export async function listPBRRequestLogs(
  filters: PBRLogFilters = {}
): Promise<PBRLogListResponse> {
  const res = await api.get<PBRLogListResponse>('/api/v1/logs', {
    params: {
      limit: filters.limit ?? 50,
      ...(filters.lane ? { lane: filters.lane } : {}),
      ...(filters.channel ? { channel: filters.channel } : {}),
      ...(filters.key ? { key: filters.key } : {}),
      ...(filters.model ? { model: filters.model } : {}),
      ...(filters.success === undefined
        ? {}
        : { success: filters.success ? 'true' : 'false' }),
      ...(filters.cursor ? { cursor: filters.cursor } : {}),
    },
  })
  return {
    items: res.data.items ?? [],
    next_cursor: res.data.next_cursor ?? null,
  }
}

/** GET /api/logs/{id}：单条（含 attempts 链）。 */
export async function getPBRRequestLog(
  id: number
): Promise<PBRRequestLogListItem> {
  const res = await api.get<PBRRequestLogListItem>(`/api/v1/logs/${id}`)
  return res.data
}
