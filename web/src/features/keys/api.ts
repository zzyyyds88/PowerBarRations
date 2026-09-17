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
  ApiKey,
  ApiResponse,
  GetApiKeysParams,
  GetApiKeysResponse,
  SearchApiKeysParams,
  ApiKeyFormData,
} from './types'

// PBR 客户端密钥适配层（api-spec §4.3 / §5.4，token-spec §3）。
//
// 控制台表格沿用基座的 ApiKey 形状，PBR 的管理面是按名称定位的 /api/keys，
// 且没有配额/分组语义：
//   - 允许的模型（model_limits）↔ lane_policy.allow_lanes（路由键即模型名）
//   - 允许的 IP ↔ ip_allowlist；过期时间 ↔ expires_at；启用状态 ↔ enabled
//   - cost 是只读统计（元），直接照抄后端响应，不参与任何写请求
// 明文只在创建/轮换时出现一次，"查看明文"通过轮换实现。

type PbrLanePolicy = {
  mode?: string
  allow_lanes?: string[]
  deny_lanes?: string[]
}

type PbrClientKey = {
  id: number
  name: string
  enabled: boolean
  lane_policy?: PbrLanePolicy
  ip_allowlist?: string[]
  rate_limit_rpm?: number
  max_concurrency?: number
  expires_at?: string | null
  notes?: string
  key_prefix?: string
  created_at?: string
  updated_at?: string
  last_used_at?: string | null
  key?: string
  // 只读统计：上游折算花费（元），由后端按 key_name 聚合 pbr_stats_hourly 派生。
  cost?: number
}

const idToName = new Map<number, string>()

function unixSeconds(value?: string | null): number {
  if (!value) return 0
  const ms = Date.parse(value)
  return Number.isFinite(ms) ? Math.floor(ms / 1000) : 0
}

function splitList(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function splitLines(value: string): string[] {
  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function toApiKey(item: PbrClientKey): ApiKey {
  idToName.set(item.id, item.name)
  const allowLanes = item.lane_policy?.allow_lanes ?? []
  return {
    id: item.id,
    name: item.name,
    key: item.key_prefix ?? '',
    status: item.enabled ? 1 : 2,
    cost: item.cost ?? 0,
    expired_time: item.expires_at ? unixSeconds(item.expires_at) : -1,
    created_time: unixSeconds(item.created_at),
    accessed_time: unixSeconds(item.last_used_at),
    model_limits_enabled: allowLanes.length > 0,
    model_limits: allowLanes.join(','),
    allow_ips: (item.ip_allowlist ?? []).join('\n'),
  }
}

async function listPbrKeys(): Promise<PbrClientKey[]> {
  const res = await api.get('/api/keys')
  const body = res.data as { items?: PbrClientKey[] }
  return body.items ?? []
}

async function nameForId(id: number): Promise<string | null> {
  if (idToName.has(id)) return idToName.get(id) ?? null
  const items = await listPbrKeys()
  for (const item of items) idToName.set(item.id, item.name)
  return idToName.get(id) ?? null
}

function toPbrPayload(data: ApiKeyFormData) {
  const allowLanes = data.model_limits_enabled
    ? splitList(data.model_limits)
    : []
  return {
    name: data.name,
    lane_policy: {
      mode: allowLanes.length > 0 ? 'allow' : 'all',
      allow_lanes: allowLanes,
      deny_lanes: [],
    },
    ip_allowlist: splitLines(data.allow_ips),
    expires_at:
      data.expired_time > 0
        ? new Date(data.expired_time * 1000).toISOString()
        : null,
  }
}

export async function getApiKeys(
  params: GetApiKeysParams = {}
): Promise<GetApiKeysResponse> {
  const { p = 1, size = 10 } = params
  const items = (await listPbrKeys()).map(toApiKey)
  const start = Math.max(0, (p - 1) * size)
  return {
    success: true,
    data: {
      items: items.slice(start, start + size),
      total: items.length,
      page: p,
      page_size: size,
    },
  }
}

export async function searchApiKeys(
  params: SearchApiKeysParams
): Promise<GetApiKeysResponse> {
  const { keyword = '', token = '', p = 1, size = 10 } = params
  const lowerKeyword = keyword.toLowerCase()
  const lowerToken = token.toLowerCase()
  const items = (await listPbrKeys()).map(toApiKey).filter((item) => {
    const matchesKeyword =
      !lowerKeyword || item.name.toLowerCase().includes(lowerKeyword)
    const matchesToken =
      !lowerToken || item.key.toLowerCase().includes(lowerToken)
    return matchesKeyword && matchesToken
  })
  const start = Math.max(0, (p - 1) * size)
  return {
    success: true,
    data: {
      items: items.slice(start, start + size),
      total: items.length,
      page: p,
      page_size: size,
    },
  }
}

export async function getApiKey(id: number): Promise<ApiResponse<ApiKey>> {
  const name = await nameForId(id)
  if (!name) return { success: false, message: 'API key not found' }
  const res = await api.get(`/api/keys/${encodeURIComponent(name)}`)
  return { success: true, data: toApiKey(res.data as PbrClientKey) }
}

export async function createApiKey(
  data: ApiKeyFormData
): Promise<ApiResponse<ApiKey>> {
  const res = await api.post('/api/keys', toPbrPayload(data))
  return { success: true, data: toApiKey(res.data as PbrClientKey) }
}

export async function updateApiKey(
  data: ApiKeyFormData & { id: number }
): Promise<ApiResponse<ApiKey>> {
  const name = await nameForId(data.id)
  if (!name) return { success: false, message: 'API key not found' }
  const payload = toPbrPayload({ ...data, name })
  const res = await api.put(`/api/keys/${encodeURIComponent(name)}`, payload)
  return { success: true, data: toApiKey(res.data as PbrClientKey) }
}

export async function deleteApiKey(id: number): Promise<ApiResponse> {
  const name = await nameForId(id)
  if (!name) return { success: false, message: 'API key not found' }
  await api.delete(`/api/keys/${encodeURIComponent(name)}`)
  return { success: true }
}

export async function batchDeleteApiKeys(
  ids: number[]
): Promise<ApiResponse<number>> {
  let deleted = 0
  for (const id of ids) {
    const result = await deleteApiKey(id)
    if (result.success) deleted += 1
  }
  return { success: true, data: deleted }
}

export async function updateApiKeyStatus(
  id: number,
  status: number
): Promise<ApiResponse<ApiKey>> {
  const name = await nameForId(id)
  if (!name) return { success: false, message: 'API key not found' }
  const res = await api.put(`/api/keys/${encodeURIComponent(name)}`, {
    enabled: status === 1,
  })
  return { success: true, data: toApiKey(res.data as PbrClientKey) }
}

// Rotate an existing key and return the new plaintext once.
export async function rotateApiKey(
  id: number
): Promise<ApiResponse<{ key: string }>> {
  const name = await nameForId(id)
  if (!name) return { success: false, message: 'API key not found' }
  const res = await api.post(`/api/keys/${encodeURIComponent(name)}/rotate`, {})
  return { success: true, data: { key: String(res.data.key ?? '') } }
}

// PBR 只在创建/轮换时返回明文；"查看明文"通过轮换获取新密钥。
export async function fetchTokenKey(
  id: number
): Promise<{ success: boolean; message?: string; data?: { key: string } }> {
  const result = await rotateApiKey(id)
  if (!result.success || !result.data?.key) {
    return { success: false, message: result.message }
  }
  return { success: true, data: { key: result.data.key } }
}

export async function fetchTokenKeysBatch(_ids: number[]): Promise<{
  success: boolean
  message?: string
  data?: { keys: Record<number, string> }
}> {
  return { success: false, message: 'PBR 不提供批量明文读取。' }
}
