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
/**
 * PBR 日志 → 上游 UsageLog 形状的映射层（ui-spec §6.6）。
 *
 * PBR 的字段结构与 new-api 的 UsageLog 不同（key_name≠token_name、
 * request_model≠model_name、total_ms(毫秒)≠use_time(秒)、estimated_cost(元)≠quota、
 * cache token/首字耗时/attempts 在顶层而非 other JSON）。本映射把 PBR 顶层字段
 * 拼成 UsageLog 形状，并构造 other JSON 供 common-logs-columns 与 DetailsDialog
 * 复用：cache_tokens/frt/admin_info.use_channel(重试链) + pbr 扩展块（车道/
 * route_source/upstream_model/error_summary/http_status/total_ms/estimated_cost/
 * attempts），供花费列与详情弹窗读取。
 */
import type { UsageLog } from '../data/schema'
import type { GetLogsResponse } from '../types'
import type {
  PBRPagedLogsResponse,
  PBRRequestLogListItem,
} from '../pbr/pbr-logs-api'

interface PBRLogOther {
  cache_tokens?: number
  cache_creation_tokens?: number
  frt?: number
  is_model_mapped?: boolean
  upstream_model_name?: string
  admin_info?: {
    // 重试链成员名数组，供 common-logs-columns 的 Channel 列 Popover 复用。
    use_channel?: string[]
  }
  pbr?: {
    lane: string
    route_source: string
    upstream_model: string
    error_kind: string
    error_summary: string
    http_status: number
    total_ms: number
    estimated_cost: number
    attempts: PBRRequestLogListItem['attempts']
    total_attempts: number
    inbound_format: string
    success: boolean
  }
}

export function mapPBRLogToUsageLog(log: PBRRequestLogListItem): UsageLog {
  const isMapped = !!(
    log.upstream_model &&
    log.upstream_model !== log.request_model
  )
  const other: PBRLogOther = {
    cache_tokens: log.cache_read_tokens || 0,
    cache_creation_tokens: log.cache_write_tokens || 0,
    frt: log.ttft_ms || 0,
    is_model_mapped: isMapped,
    upstream_model_name: log.upstream_model || undefined,
    admin_info: {
      use_channel: (log.attempts ?? [])
        .map((a) => a.member)
        .filter(Boolean),
    },
    pbr: {
      lane: log.lane,
      route_source: log.route_source,
      upstream_model: log.upstream_model,
      error_kind: log.error_kind,
      error_summary: log.error_summary,
      http_status: log.http_status,
      total_ms: log.total_ms,
      estimated_cost: log.estimated_cost,
      attempts: log.attempts ?? [],
      total_attempts: log.total_attempts,
      inbound_format: log.inbound_format,
      success: log.success,
    },
  }
  return {
    id: log.id,
    user_id: log.user_id,
    created_at: Math.floor(Date.parse(log.ts) / 1000),
    type: log.type,
    content: log.error_summary || '',
    username: log.username,
    token_name: log.key_name,
    model_name: log.request_model,
    quota: 0,
    prompt_tokens: log.prompt_tokens,
    completion_tokens: log.completion_tokens,
    use_time: Math.floor(log.total_ms / 1000),
    is_stream: log.is_stream,
    channel: log.channel_id,
    channel_name: log.channel,
    token_id: log.token_id,
    group: '',
    ip: log.ip,
    other: JSON.stringify(other),
    request_id: '',
    upstream_request_id: '',
  }
}

export function mapPBRLogsResponse(res: PBRPagedLogsResponse): GetLogsResponse {
  return {
    items: (res.items ?? []).map(mapPBRLogToUsageLog),
    total: res.total ?? 0,
    page: res.page,
    page_size: res.page_size,
  }
}
