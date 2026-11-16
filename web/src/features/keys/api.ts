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
// 密钥明文入库（token-spec §3.3，New API 同款"随时可看"）：列表即回传 key 明文；
// 迁移前的历史密钥没有明文（key 为 null），回显走 key_prefix 并提示轮换一次。

type PbrLanePolicy = {
  mode?: string
  allow_lanes?: string[]
  deny_lanes?: string[]
}

/** 批量删除结果：分别汇报成功数、失败数与失败令牌名，供界面保留重试选择。 */
export interface BatchDeleteApiKeysResult {
  deleted: number
  failed: number
  failedNames: string[]
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
  const denyLanes = item.lane_policy?.deny_lanes ?? []
  return {
    id: item.id,
    name: item.name,
    // `key` is null only for keys created before plaintext storage was enabled.
    key: item.key ?? item.key_prefix ?? '',
    key_plain: item.key ?? '',
    status: item.enabled ? 1 : 2,
    cost: item.cost ?? 0,
    expired_time: item.expires_at ? unixSeconds(item.expires_at) : -1,
    created_time: unixSeconds(item.created_at),
    accessed_time: unixSeconds(item.last_used_at),
    model_limits_enabled: allowLanes.length > 0,
    model_limits: allowLanes.join(','),
    deny_lanes: denyLanes.join(','),
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
      // 保留用户未改动的拒绝清单：表单不编辑它，这里原样回写，
      // 避免「mode=all + deny_lanes=[x]」在保存后被静默放宽为全部允许。
      deny_lanes: data.deny_lanes ?? [],
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

/**
 * 批量删除：逐条删除并用 allSettled 汇总，绝不把部分失败假报成全部成功。
 * 失败项保留其令牌名，调用方据此保留选择让用户重试。
 */
export async function batchDeleteApiKeys(
  ids: number[]
): Promise<ApiResponse<BatchDeleteApiKeysResult>> {
  const results = await Promise.allSettled(
    ids.map(async (id) => {
      const name = (await nameForId(id)) ?? String(id)
      const result = await deleteApiKey(id)
      if (!result.success) throw new Error(result.message || name)
      return name
    })
  )

  let deleted = 0
  const failedNames: string[] = []
  results.forEach((result, index) => {
    if (result.status === 'fulfilled') {
      deleted += 1
      return
    }
    const id = ids[index]
    failedNames.push(idToName.get(id) ?? String(id))
  })

  return {
    success: true,
    data: { deleted, failed: failedNames.length, failedNames },
  }
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

// Rotate an existing key, persist the replacement, and return its plaintext.
export async function rotateApiKey(
  id: number
): Promise<ApiResponse<{ key: string }>> {
  const name = await nameForId(id)
  if (!name) return { success: false, message: 'API key not found' }
  const res = await api.post(`/api/keys/${encodeURIComponent(name)}/rotate`, {})
  return { success: true, data: { key: String(res.data.key ?? '') } }
}

// 读取已入库的明文（token-spec §3.3，明文随时可看）：走详情接口拿 `key`。
// 历史密钥（明文存储上线前创建）无明文，返回失败并提示轮换一次。
export async function fetchTokenKey(
  id: number
): Promise<{ success: boolean; message?: string; data?: { key: string } }> {
  const name = await nameForId(id)
  if (!name) return { success: false, message: 'API key not found' }
  const res = await api.get(`/api/keys/${encodeURIComponent(name)}`)
  const plain = (res.data as PbrClientKey)?.key
  if (!plain) {
    return {
      success: false,
      message:
        'This key predates plaintext storage; rotate it once to reveal and copy.',
    }
  }
  return { success: true, data: { key: plain } }
}

// 批量读取明文：逐个走 fetchTokenKey，失败的历史密钥跳过（不阻断其余）。
export async function fetchTokenKeysBatch(ids: number[]): Promise<{
  success: boolean
  message?: string
  data?: { keys: Record<number, string> }
}> {
  const keys: Record<number, string> = {}
  await Promise.all(
    ids.map(async (id) => {
      const res = await fetchTokenKey(id)
      if (res.success && res.data?.key) keys[id] = res.data.key
    })
  )
  return { success: true, data: { keys } }
}
