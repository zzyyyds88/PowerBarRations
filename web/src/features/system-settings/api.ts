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

import type {
  LogCleanupTask,
  SystemOption,
  SystemTask,
  SystemTaskResponse,
  SystemTaskListResponse,
  UpdateOptionRequest,
} from './types'

/** `GET /api/option/`（基座面）成功即裸选项数组；稳定面 `/api/system/options/all` 是 `{items}`。 */
export async function getSystemOptions(): Promise<SystemOption[]> {
  const res = await api.get<SystemOption[] | { items?: SystemOption[] }>(
    '/api/option/'
  )
  const body = res.data
  if (Array.isArray(body)) return body
  return body?.items ?? []
}

/** `PUT /api/option/` 成功无实体（204）。 */
export async function updateSystemOption(
  request: UpdateOptionRequest
): Promise<void> {
  await api.put('/api/option/', request)
}

/**
 * `POST /api/system-task/log-cleanup` 成功即裸任务对象。
 */
export async function startLogCleanupTask(
  targetTimestamp: number
): Promise<SystemTaskResponse<LogCleanupTask>> {
  const res = await api.post<SystemTaskResponse<LogCleanupTask>>(
    '/api/system-task/log-cleanup',
    null,
    {
      params: { target_timestamp: targetTimestamp },
    }
  )
  return res.data
}

/**
 * `GET /api/system-task/current` 成功即裸任务对象；无任务时后端回 204（无实体）。
 */
export async function getCurrentLogCleanupTask(): Promise<
  SystemTaskResponse<LogCleanupTask | null>
> {
  const res = await api.get<SystemTaskResponse<LogCleanupTask | null>>(
    '/api/system-task/current',
    {
      params: { type: 'log_cleanup' },
    }
  )
  return res.data ?? null
}

/** `GET /api/system-task/{id}` 成功即裸任务对象。 */
export async function getSystemTask(
  taskId: string
): Promise<SystemTaskResponse<LogCleanupTask>> {
  const res = await api.get<SystemTaskResponse<LogCleanupTask>>(
    `/api/system-task/${taskId}`
  )
  return res.data
}

/** `GET /api/system-task/list` 成功即裸任务数组。 */
export async function listSystemTasks(
  limit = 20
): Promise<SystemTaskListResponse> {
  const res = await api.get<SystemTask[] | { items?: SystemTask[] }>(
    '/api/system-task/list',
    { params: { limit } }
  )
  const body = res.data
  if (Array.isArray(body)) return body
  return body?.items ?? []
}

export type { SystemTask }
