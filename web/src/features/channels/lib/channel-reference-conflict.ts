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
import {
  getServerErrorDetails,
  getServerErrorSources,
} from '@/lib/server-error-message'

/**
 * 车道 / 成员组引用冲突（api-spec §3 details）的统一解析。
 *
 * 权威来源是 `error.details`（稳定面）或基座信封 `data`（基座面）里的：
 *   - `lanes` / `groups`：单条冲突，引用该渠道的车道名 / 成员组名；
 *   - `blocked`：批量冲突，渠道名 → 引用它的车道名；
 *   - `groups`（对象形态）：批量冲突，渠道名 → 引用它的成员组名。
 *
 * ADR 0009 之后车道可以只引用成员组，两份额外清单必须一起解析，否则删除/收窄
 * 模型时会出现"接口已拒绝、界面却只显示删除失败"或错误地继续清理散成员。
 *
 * message 尾部解析只作兜底（老后端/代理改写导致 details 丢失时），不作为唯一来源。
 */
export interface ChannelReferenceConflict {
  code?: string
  message?: string
  /** 单条冲突：引用该渠道的车道名。 */
  lanes: string[]
  /** 单条冲突：引用该渠道的成员组名。 */
  groups: string[]
  /** 批量冲突：渠道名 → 引用它的车道名。 */
  blocked: Record<string, string[]>
  /** 批量冲突：渠道名 → 引用它的成员组名。 */
  blockedGroups: Record<string, string[]>
}

const REFERENCED_MODELS_MESSAGE_PATTERN =
  /仍被车道或成员组引用|referenced by lanes or member groups|still referenced by lanes/i

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function asStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === 'string')
}

function asStringMap(value: unknown): Record<string, string[]> {
  if (!isRecord(value)) return {}
  const out: Record<string, string[]> = {}
  for (const [key, item] of Object.entries(value)) {
    const list = asStringArray(item)
    if (list.length > 0) out[key] = list
  }
  return out
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

/**
 * 从任意失败（axios 拒绝或原始响应体）解析引用明细。
 *
 * 同时扫描 `details`、`error.details`、基座信封 `data` 与原始 payload：稳定面
 * 走 `{error:{details}}`，基座面走 `{success:false,data:{...}}`，两套都要能读。
 */
export function parseChannelReferenceConflict(
  error: unknown,
  fallbackMessage = ''
): ChannelReferenceConflict {
  const details = getServerErrorDetails(error, fallbackMessage)
  let lanes: string[] = []
  let groups: string[] = []
  let blocked: Record<string, string[]> = {}
  let blockedGroups: Record<string, string[]> = {}

  const candidates: Record<string, unknown>[] = []
  for (const source of getServerErrorSources(error)) {
    if (isRecord(source.details)) candidates.push(source.details)
    if (isRecord(source.error) && isRecord(source.error.details)) {
      candidates.push(source.error.details)
    }
    if (isRecord(source.data)) candidates.push(source.data)
  }
  if (isRecord(details.details)) candidates.push(details.details)

  for (const record of candidates) {
    if (lanes.length === 0) lanes = asStringArray(record.lanes)
    if (groups.length === 0 && Array.isArray(record.groups)) {
      groups = asStringArray(record.groups)
    }
    if (Object.keys(blocked).length === 0) {
      blocked = asStringMap(record.blocked)
    }
    if (Object.keys(blockedGroups).length === 0) {
      blockedGroups = asStringMap(record.blocked_groups)
      if (Object.keys(blockedGroups).length === 0) {
        blockedGroups = asStringMap(
          (record as { blockedGroups?: unknown }).blockedGroups
        )
      }
      if (Object.keys(blockedGroups).length === 0 && isRecord(record.groups)) {
        blockedGroups = asStringMap(record.groups)
      }
    }
  }
  if (
    lanes.length === 0 &&
    Object.keys(blocked).length === 0 &&
    groups.length === 0 &&
    Object.keys(blockedGroups).length === 0
  ) {
    lanes = lanesFromMessage(details.message)
  }
  return {
    code: details.code,
    message: details.message,
    lanes,
    groups,
    blocked,
    blockedGroups,
  }
}

/**
 * 是否属于"引用"冲突：优先看明细，其次接受两个稳定 code，最后才回落到 message。
 */
export function isChannelReferenceConflict(error: unknown): boolean {
  const conflict = parseChannelReferenceConflict(error)
  if (
    conflict.lanes.length > 0 ||
    conflict.groups.length > 0 ||
    Object.keys(conflict.blocked).length > 0 ||
    Object.keys(conflict.blockedGroups).length > 0
  ) {
    return true
  }
  if (
    conflict.code === 'conflict' ||
    conflict.code === 'models_referenced_by_lanes' ||
    conflict.code === 'models_referenced_by_groups'
  ) {
    return true
  }
  return REFERENCED_MODELS_MESSAGE_PATTERN.test(conflict.message ?? '')
}
