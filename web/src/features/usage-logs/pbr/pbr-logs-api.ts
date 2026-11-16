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

/** 上下文分区（api-spec §4.4 / usage-spec §5）。只含名称/token，不含正文。 */
export interface PBRContextItem {
  name: string
  tag?: string
  n?: number
  tokens: number
}

export interface PBRContextPart {
  kind: string
  tokens: number
  items?: PBRContextItem[]
}

export type PBRContextStatus =
  | 'ok'
  | 'unsupported'
  | 'too_large'
  | 'unavailable'
  | ''

export interface PBRContext {
  version: number
  status: PBRContextStatus
  tokens: number
  estimated_tokens: number
  window: number
  counted: boolean
  held: boolean
  turns: number
  truncated: boolean
  parts: PBRContextPart[]
}

/** GET /api/logs 的元素（api-spec §5.5 的元数据口径）。 */
export interface PBRRequestLogListItem {
  id: number
  ts: string
  lane: string
  request_model: string
  route_source: string
  channel: string
  channel_id: number
  upstream_model: string
  key_name: string
  token_id: number
  /** 统一日志表（design-v1 §8）：管理动作（渠道测试等）记录管理员，模型面请求为空。 */
  user_id: number
  username: string
  /** 消耗（2）/错误（5）。 */
  type: number
  ip: string
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
  /** 用量元数据（usage-spec §3–§6）：列表只带上下文摘要。 */
  session_id?: string
  session_key?: string
  subagent?: string
  parent_subagent?: string
  parent_session_id?: string
  client?: string
  client_group?: string
  client_name?: string
  context_tokens?: number
  context_window?: number
  context_counted?: boolean
  context_status?: PBRContextStatus
  /** 隐私脱敏摘要（privacy-spec §7）；命中明细只在详情/脱敏页返回。 */
  masked_count?: number
  mask_blocked?: boolean
  mask_status?: string
  mask_took_ms?: number
  /** 详情接口才返回完整分区；列表项没有该字段。 */
  context?: PBRContext | null
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
  client?: string
  session?: string
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
      ...(filters.client ? { client: filters.client } : {}),
      ...(filters.session ? { session: filters.session } : {}),
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

/** GET /api/logs/{id}：单条（含 attempts 链与完整上下文分区）。 */
export interface PBRMaskHit {
  kind: string
  by: string
  placeholder: string
  head?: string
  len: number
  n: number
  action?: string
}

export interface PBRRequestLogDetail extends PBRRequestLogListItem {
  context?: PBRContext | null
  mask_hits?: PBRMaskHit[]
}

export async function getPBRRequestLog(
  id: number
): Promise<PBRRequestLogDetail> {
  const res = await api.get<PBRRequestLogDetail>(`/api/v1/logs/${id}`)
  return res.data
}

/** GET /api/logs 偏移分页（api-spec §5.5）：响应含 total/page/page_size，供控制台跳页/总数。 */
export interface PBRPagedLogsResponse {
  items: PBRRequestLogListItem[]
  total: number
  page: number
  page_size: number
}

export async function listPBRRequestLogsPaged(params: {
  page: number
  page_size: number
  lane?: string
  channel?: string
  key?: string
  model?: string
  client?: string
  session?: string
  success?: boolean
  masked?: boolean
  context?: boolean
  since?: number
  until?: number
}): Promise<PBRPagedLogsResponse> {
  const res = await api.get<PBRPagedLogsResponse>('/api/v1/logs', {
    params: {
      page: params.page,
      page_size: params.page_size,
      ...(params.lane ? { lane: params.lane } : {}),
      ...(params.channel ? { channel: params.channel } : {}),
      ...(params.key ? { key: params.key } : {}),
      ...(params.model ? { model: params.model } : {}),
      ...(params.client ? { client: params.client } : {}),
      ...(params.session ? { session: params.session } : {}),
      ...(params.success === undefined
        ? {}
        : { success: params.success ? 'true' : 'false' }),
      ...(params.masked === undefined
        ? {}
        : { masked: params.masked ? 'true' : 'false' }),
      ...(params.context === undefined
        ? {}
        : { context: params.context ? 'true' : 'false' }),
      ...(params.since ? { since: params.since } : {}),
      ...(params.until ? { until: params.until } : {}),
    },
  })
  return {
    items: res.data.items ?? [],
    total: res.data.total ?? 0,
    page: res.data.page ?? params.page,
    page_size: res.data.page_size ?? params.page_size,
  }
}
