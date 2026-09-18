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
import { getServerErrorDetails } from '@/lib/server-error-message'

/**
 * 车道引用冲突（api-spec §3 details）的统一解析。
 *
 * 权威来源是 **`error.details.lanes`**（单条/收窄模型：引用该渠道的车道名）与
 * **`error.details.blocked`**（批量：渠道名 → 引用它的车道名）。两个 code 都可能出现：
 *   - `conflict`：稳定面 PUT/DELETE /api/channels/{name}、批量删除；
 *   - `models_referenced_by_lanes`：基座面 PUT /api/channel/ 收窄模型。
 *
 * message 尾部解析只作兜底（老后端/代理改写导致 details 丢失时），不作为唯一来源。
 */
export interface ChannelReferenceConflict {
  code?: string
  message?: string
  /** 单条冲突：引用该渠道的车道名。 */
  lanes: string[]
  /** 批量冲突：渠道名 → 引用它的车道名。 */
  blocked: Record<string, string[]>
}

const REFERENCED_MODELS_MESSAGE_PATTERN = /仍被车道引用|referenced by lanes/i

function asStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === 'string')
}

/** 兜底：基座面只把车道名写进 message（`…：lane-a, lane-b`）。 */
function lanesFromMessage(message?: string): string[] {
  const separator = message?.search(/[:：]/) ?? -1
  if (separator < 0) return []
  return (message ?? '')
    .slice(separator + 1)
    .split(/[,，]/)
    .map((lane) => lane.trim())
    .filter(Boolean)
}

/** 从任意失败（axios 拒绝或原始响应体）解析车道引用冲突明细。 */
export function parseChannelReferenceConflict(
  error: unknown,
  fallbackMessage = ''
): ChannelReferenceConflict {
  const details = getServerErrorDetails(error, fallbackMessage)
  const raw = details.details
  let lanes: string[] = []
  let blocked: Record<string, string[]> = {}
  if (raw && typeof raw === 'object') {
    const record = raw as { lanes?: unknown; blocked?: unknown }
    lanes = asStringArray(record.lanes)
    if (record.blocked && typeof record.blocked === 'object') {
      blocked = record.blocked as Record<string, string[]>
    }
  }
  if (lanes.length === 0 && Object.keys(blocked).length === 0) {
    lanes = lanesFromMessage(details.message)
  }
  return { code: details.code, message: details.message, lanes, blocked }
}

/**
 * 是否属于"车道引用"冲突：优先看 details 是否有 lanes/blocked，其次接受两个稳定 code，
 * 最后才回落到 message。
 */
export function isChannelReferenceConflict(error: unknown): boolean {
  const conflict = parseChannelReferenceConflict(error)
  if (conflict.lanes.length > 0 || Object.keys(conflict.blocked).length > 0) {
    return true
  }
  if (
    conflict.code === 'conflict' ||
    conflict.code === 'models_referenced_by_lanes'
  ) {
    return true
  }
  return REFERENCED_MODELS_MESSAGE_PATTERN.test(conflict.message ?? '')
}
