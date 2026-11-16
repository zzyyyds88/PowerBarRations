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

import type { PBRRequestLogListItem } from './pbr-logs-api'

/** GET /api/logs/sessions 的元素（api-spec §4.6）。 */
export interface PBRSession {
  session_id: string
  session_key: string
  first_ts: string
  last_ts: string
  requests: number
  successes: number
  errors: number
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  reasoning_tokens: number
  estimated_cost: number
  clients: string[]
  client_groups: string[]
  models: string[]
  lanes: string[]
  last_model: string
  last_client: string
  context_peak_tokens: number
  context_window: number
  context_requests: number
  parent_session_id: string
}

export interface PBRSessionWindowSummary {
  sessions: number
  requests: number
  successes: number
  errors: number
  prompt_tokens: number
  completion_tokens: number
  estimated_cost: number
}

export interface PBRSessionListResponse {
  items: PBRSession[]
  total: number
  page: number
  page_size: number
  summary: PBRSessionWindowSummary
}

export interface PBRSessionContextPoint {
  ts: string
  id: number
  tokens: number
  window: number
}

/** A session timeline request: the log plus the compact context. */
export interface PBRSessionCompactContext {
  tokens: number
  window: number
  counted: boolean
  status: string
  parts: { kind: string; tokens: number }[]
}

export type PBRSessionRequest = Omit<PBRRequestLogListItem, 'context'> & {
  context?: PBRSessionCompactContext | null
}

export interface PBRSessionDetail extends PBRSession {
  request_items: PBRSessionRequest[]
  total: number
  page: number
  page_size: number
  context_series: PBRSessionContextPoint[]
  context_series_truncated: boolean
}

export type PBRSessionSort =
  | 'last_ts'
  | 'first_ts'
  | 'requests'
  | 'tokens'
  | 'cost'

export interface PBRSessionListParams {
  from?: number
  to?: number
  client?: string
  model?: string
  lane?: string
  q?: string
  sort?: PBRSessionSort
  order?: 'asc' | 'desc'
  page?: number
  page_size?: number
}

export async function listPBRSessions(
  params: PBRSessionListParams = {}
): Promise<PBRSessionListResponse> {
  const res = await api.get<PBRSessionListResponse>('/api/v1/logs/sessions', {
    params: {
      ...(params.from ? { from: params.from } : {}),
      ...(params.to ? { to: params.to } : {}),
      ...(params.client ? { client: params.client } : {}),
      ...(params.model ? { model: params.model } : {}),
      ...(params.lane ? { lane: params.lane } : {}),
      ...(params.q ? { q: params.q } : {}),
      sort: params.sort ?? 'last_ts',
      order: params.order ?? 'desc',
      page: params.page ?? 1,
      page_size: params.page_size ?? 50,
    },
  })
  return {
    items: res.data.items ?? [],
    total: res.data.total ?? 0,
    page: res.data.page ?? params.page ?? 1,
    page_size: res.data.page_size ?? params.page_size ?? 50,
    summary: res.data.summary ?? {
      sessions: 0,
      requests: 0,
      successes: 0,
      errors: 0,
      prompt_tokens: 0,
      completion_tokens: 0,
      estimated_cost: 0,
    },
  }
}

export async function getPBRSession(
  key: string,
  params: { page?: number; page_size?: number; order?: 'asc' | 'desc' } = {}
): Promise<PBRSessionDetail> {
  const res = await api.get<PBRSessionDetail>(
    `/api/v1/logs/sessions/${encodeURIComponent(key)}`,
    {
      params: {
        page: params.page ?? 1,
        page_size: params.page_size ?? 50,
        order: params.order ?? 'desc',
      },
    }
  )
  return res.data
}
