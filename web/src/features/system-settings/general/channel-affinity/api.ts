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

import type { CacheStats } from './types'

/** `GET /api/option/channel_affinity_cache` 成功即裸缓存统计。 */
export async function getCacheStats(): Promise<CacheStats> {
  const res = await api.get<CacheStats>('/api/option/channel_affinity_cache', {
    disableDuplicate: true,
  } as Record<string, unknown>)
  return res.data
}

/** 清空全部亲和缓存；成功体为 `{deleted}`。 */
export async function clearAllCache(): Promise<{ deleted: number }> {
  const res = await api.delete<{ deleted: number }>(
    '/api/option/channel_affinity_cache',
    {
      params: { all: true },
    }
  )
  return res.data
}

/** 清空单条规则缓存；成功体为 `{deleted}`。 */
export async function clearRuleCache(
  ruleName: string
): Promise<{ deleted: number }> {
  const res = await api.delete<{ deleted: number }>(
    '/api/option/channel_affinity_cache',
    {
      params: { rule_name: ruleName },
    }
  )
  return res.data
}

/** `GET /api/log/channel_affinity_usage_cache` 成功即裸明细对象。 */
export async function getAffinityUsageCache(params: {
  rule_name: string
  using_group: string
  key_hint: string
  key_fp: string
}): Promise<unknown> {
  const res = await api.get('/api/log/channel_affinity_usage_cache', {
    params,
    disableDuplicate: true,
  } as Record<string, unknown>)
  return res.data
}
