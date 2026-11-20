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
import z from 'zod'

/**
 * Search schemas for console workspaces.
 *
 * Parameter names are a URL compatibility contract: old paths redirect into
 * these routes with search + hash preserved. Do not rename keys without a
 * migration plan.
 */

export const channelsSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
  filter: z.string().optional().catch(''),
  status: z.array(z.string()).optional().catch([]),
  protocol: z.array(z.string()).optional().catch([]),
  model: z.string().optional().catch(''),
})

export const apiKeysSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
  status: z.array(z.string()).optional().catch([]),
  filter: z.string().optional().catch(''),
  token: z.string().optional().catch(''),
})

export const consoleModelsSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
  filter: z.string().optional().catch(''),
})

const logTypeValues = ['0', '1', '2', '3', '4', '5', '6', '7'] as const
const logTypeSearchSchema = z
  .preprocess((value) => {
    if (value == null || value === '') return undefined
    return Array.isArray(value) ? value : [value]
  }, z.array(z.enum(logTypeValues)).optional())
  .catch([])

export const consoleLogsSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
  type: logTypeSearchSchema.optional(),
  filter: z.string().optional().catch(''),
  model: z.string().optional().catch(''),
  token: z.string().optional().catch(''),
  channel: z.string().optional().catch(''),
  group: z.string().optional().catch(''),
  username: z.string().optional().catch(''),
  requestId: z.string().optional().catch(''),
  upstreamRequestId: z.string().optional().catch(''),
  startTime: z.number().optional(),
  endTime: z.number().optional(),
  lane: z.string().optional().catch(''),
  key: z.string().optional().catch(''),
  success: z.enum(['true', 'false']).optional().catch(undefined),
})

export const channelHealthSearchSchema = z.object({
  window: z.enum(['24h', '7d']).optional().catch('24h'),
})

export const overviewSearchSchema = z.object({
  range: z
    .enum(['24h', 'today', '3d', '7d', '30d', 'custom'])
    .optional()
    .catch('24h'),
  from: z.number().optional(),
  to: z.number().optional(),
  // 实时路由选中的车道名（动态值，不能用 enum）；非法/旧链接回落空串，
  // 由概览按当前窗口流量自动选一条默认车道。概览只把它转给 /live-routing。
  lane: z.string().optional().catch(''),
})

// 实时路由是观察面：只保留 `lane` 深链。历史上的 `range`/`from`/`to`/`window`
// 参数不再读取（旧链接由 zod 丢弃），因为该页的窗口已固定为实时口径。
export const liveRoutingSearchSchema = z.object({
  lane: z.string().optional().catch(''),
})
