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
import { api, type ApiRequestConfig } from '@/lib/api'

import type {
  AddChannelRequest,
  BatchDeleteParams,
  BatchSetTagParams,
  Channel,
  ChannelTestResponse,
  ChannelUpdateResponse,
  CopyChannelParams,
  CopyChannelResponse,
  FetchModelsResponse,
  GetChannelResponse,
  GetChannelsParams,
  GetChannelsResponse,
  MultiKeyManageParams,
  MultiKeyStatusResponse,
  SearchChannelsParams,
  SearchChannelsResponse,
  TagOperationParams,
} from './types'

// 基座面写操作使用 skipErrorHandler：调用方自己决定失败时展示什么（api-spec §3 的
// 失败包络由 axios 拒绝承载，不再是 200 + success:false）。
const channelActionConfig = (
  config: ApiRequestConfig = {}
): ApiRequestConfig => ({
  ...config,
  skipErrorHandler: true,
})

// ============================================================================
// Base Channel CRUD Operations
// ============================================================================

/**
 * Get paginated list of channels.
 *
 * 基座面 `GET /api/channel/` 仍返回 `{items,total,page,page_size,type_counts}`
 * （分页参数由请求端控制），不是稳定面的 cursor 形状；见 types 里的契约注释。
 */
export async function getChannels(
  params: GetChannelsParams = {}
): Promise<GetChannelsResponse> {
  const res = await api.get('/api/channel/', { params })
  return res.data
}

/**
 * Search channels with filters
 */
export async function searchChannels(
  params: SearchChannelsParams
): Promise<SearchChannelsResponse> {
  const res = await api.get('/api/channel/search', { params })
  return res.data
}

/**
 * Get single channel by ID
 */
export async function getChannel(id: number): Promise<GetChannelResponse> {
  const res = await api.get(`/api/channel/${id}`)
  return res.data
}

export async function getChannelDefaultBaseURLs(): Promise<
  Partial<Record<number, string>>
> {
  const response = await api.get<Partial<Record<number, string>>>(
    '/api/channel/default_base_urls'
  )
  return response.data ?? {}
}

/**
 * Create new channel(s)
 * Supports single, batch, and multi-key modes
 */
export async function createChannel(data: AddChannelRequest): Promise<void> {
  await api.post('/api/channel', data, channelActionConfig())
}

/**
 * Update existing channel
 */
export async function updateChannel(
  id: number,
  data: Partial<Channel> & { cleanup_models?: boolean }
): Promise<ChannelUpdateResponse> {
  const res = await api.put(
    '/api/channel/',
    { id, ...data },
    channelActionConfig()
  )
  return res.data
}

/**
 * Update channel enabled/disabled status.
 *
 * 成功体是裸布尔（基座面 data 裸化），失败由 axios 拒绝。
 */
export async function updateChannelStatus(
  id: number,
  status: number
): Promise<boolean> {
  const res = await api.post(
    `/api/channel/${id}/status`,
    { status },
    channelActionConfig()
  )
  return Boolean(res.data)
}

/**
 * Batch update channel enabled/disabled status.
 *
 * 成功体是裸变更数。
 */
export async function batchUpdateChannelStatus(
  ids: number[],
  status: number
): Promise<number> {
  const res = await api.post(
    '/api/channel/status/batch',
    { ids, status },
    channelActionConfig()
  )
  return typeof res.data === 'number' ? res.data : 0
}

/**
 * Delete single channel
 */
export async function deleteChannel(id: number): Promise<void> {
  await api.delete(`/api/channel/${id}`, channelActionConfig())
}

/**
 * Batch delete channels
 *
 * 成功体是裸删除数（失败时整批拒绝并返回 409 + details.blocked）。
 */
export async function batchDeleteChannels(
  data: BatchDeleteParams
): Promise<number> {
  const res = await api.post('/api/channel/batch', data, channelActionConfig())
  return typeof res.data === 'number' ? res.data : 0
}

/**
 * Batch set tag for channels
 */
export async function batchSetChannelTag(
  data: BatchSetTagParams
): Promise<number> {
  const res = await api.post(
    '/api/channel/batch/tag',
    data,
    channelActionConfig()
  )
  return typeof res.data === 'number' ? res.data : 0
}

// ============================================================================
// Channel Operations
// ============================================================================

/**
 * Test channel connectivity
 */
export async function testChannel(
  id: number,
  params?: { model?: string; endpoint_type?: string; stream?: boolean }
): Promise<ChannelTestResponse> {
  const res = await api.get(
    `/api/channel/test/${id}`,
    channelActionConfig({ params })
  )
  return res.data
}

/**
 * Fetch available models from upstream provider
 */
export async function fetchUpstreamModels(
  id: number
): Promise<FetchModelsResponse> {
  const res = await api.get(
    `/api/channel/fetch_models/${id}`,
    channelActionConfig()
  )
  return res.data
}

/**
 * Copy/clone a channel
 */
export async function copyChannel(
  id: number,
  params: CopyChannelParams = {}
): Promise<CopyChannelResponse> {
  const res = await api.post(
    `/api/channel/copy/${id}`,
    null,
    channelActionConfig({ params })
  )
  return res.data
}

/**
 * Fix channel abilities
 */
export async function fixChannelAbilities(): Promise<{
  success: number
  fails: number
}> {
  const res = await api.post(
    '/api/channel/fix',
    undefined,
    channelActionConfig()
  )
  return res.data
}

/**
 * Delete all disabled channels
 *
 * 成功体是裸删除数；被车道引用时整批拒绝（409 + details.blocked）。
 */
export async function deleteDisabledChannels(): Promise<number> {
  const res = await api.delete('/api/channel/disabled', channelActionConfig())
  return typeof res.data === 'number' ? res.data : 0
}

/**
 * Get channel key (requires 2FA verification)
 */
export async function getChannelKey(
  id: number,
  proofToken: string,
  signal?: AbortSignal
): Promise<{ key: string }> {
  const res = await api.post(
    `/api/channel/${id}/key`,
    undefined,
    channelActionConfig({
      headers: { 'X-Security-Proof': proofToken },
      signal,
    })
  )
  return res.data
}

// ============================================================================
// Codex Channel Operations
// ============================================================================

export async function refreshCodexCredential(
  channelId: number
): Promise<CodexCredentialRefreshResponse> {
  const res = await api.post(
    `/api/channel/${channelId}/codex/refresh`,
    {},
    channelActionConfig()
  )
  return res.data
}

export async function getCodexUsage(
  channelId: number
): Promise<CodexUsageResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/codex/usage`,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

export async function getCodexResetCredits(
  channelId: number
): Promise<CodexUsageResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/codex/usage/reset-credits`,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

export async function resetCodexUsage(
  channelId: number
): Promise<CodexUsageResponse> {
  const res = await api.post(
    `/api/channel/${channelId}/codex/usage/reset`,
    {},
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

// ============================================================================
// Multi-Key Management
// ============================================================================

/**
 * Manage multi-key channel operations.
 *
 * `get_key_status` 成功为分页对象；其余动作成功为 `{applied:true,message}`。
 * 两者都直接是裸资源。
 */
export async function manageMultiKeys(
  params: MultiKeyManageParams
): Promise<MultiKeyStatusResponse | { applied: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/multi_key/manage',
    params,
    channelActionConfig()
  )
  return res.data
}

/**
 * Get key status for multi-key channel
 */
export async function getMultiKeyStatus(
  channelId: number,
  page = 1,
  pageSize = 50,
  status?: number
): Promise<MultiKeyStatusResponse> {
  const res = await manageMultiKeys({
    channel_id: channelId,
    action: 'get_key_status',
    page,
    page_size: pageSize,
    status,
  })
  return res as MultiKeyStatusResponse
}

/**
 * Enable a specific key in multi-key channel
 */
export async function enableMultiKey(
  channelId: number,
  keyIndex: number
): Promise<void> {
  await manageMultiKeys({
    channel_id: channelId,
    action: 'enable_key',
    key_index: keyIndex,
  })
}

/**
 * Disable a specific key in multi-key channel
 */
export async function disableMultiKey(
  channelId: number,
  keyIndex: number
): Promise<void> {
  await manageMultiKeys({
    channel_id: channelId,
    action: 'disable_key',
    key_index: keyIndex,
  })
}

/**
 * Delete a specific key in multi-key channel
 */
export async function deleteMultiKey(
  channelId: number,
  keyIndex: number
): Promise<void> {
  await manageMultiKeys({
    channel_id: channelId,
    action: 'delete_key',
    key_index: keyIndex,
  })
}

/**
 * Enable all keys in multi-key channel
 */
export async function enableAllMultiKeys(channelId: number): Promise<void> {
  await manageMultiKeys({
    channel_id: channelId,
    action: 'enable_all_keys',
  })
}

/**
 * Disable all keys in multi-key channel
 */
export async function disableAllMultiKeys(channelId: number): Promise<void> {
  await manageMultiKeys({
    channel_id: channelId,
    action: 'disable_all_keys',
  })
}

/**
 * Delete all disabled keys in multi-key channel
 */
export async function deleteDisabledMultiKeys(
  channelId: number
): Promise<{ applied: boolean; message?: string }> {
  const res = await manageMultiKeys({
    channel_id: channelId,
    action: 'delete_disabled_keys',
  })
  return res as { applied: boolean; message?: string }
}

// ============================================================================
// Tag Operations
// ============================================================================

/**
 * Enable all channels with a specific tag
 */
export async function enableTagChannels(tag: string): Promise<void> {
  await api.post('/api/channel/tag/enabled', { tag }, channelActionConfig())
}

/**
 * Disable all channels with a specific tag
 */
export async function disableTagChannels(tag: string): Promise<void> {
  await api.post('/api/channel/tag/disabled', { tag }, channelActionConfig())
}

/**
 * Edit all channels with a specific tag
 */
export async function editTagChannels(
  params: TagOperationParams
): Promise<void> {
  await api.put('/api/channel/tag', params, channelActionConfig())
}

/**
 * Get models for a specific tag（成功体是逗号分隔的裸字符串）。
 */
export async function getTagModels(tag: string): Promise<string> {
  const res = await api.get('/api/channel/tag/models', { params: { tag } })
  return typeof res.data === 'string' ? res.data : ''
}

// ============================================================================
// Utility Functions
// ============================================================================

/**
 * Fetch models from the current unsaved channel form configuration.
 */
export async function fetchModels(data: {
  base_url: string
  type: number
  key?: string
  channel_id?: number
  advanced_custom?: string
  header_override?: string
  proxy?: string
}): Promise<FetchModelsResponse> {
  const res = await api.post(
    '/api/channel/fetch_models',
    data,
    channelActionConfig()
  )
  return res.data
}

/**
 * Fetch the upstream model list through the stable ops surface.
 *
 * 稳定面 `POST /api/channels/batch/fetch-models`（api-spec §5.3.1）按**渠道名**
 * 或**草稿连接信息**拉取，不落库：成功即裸 `{models:[...]}`，上游失败由 axios
 * 以真实状态码（502 `upstream_error`）拒绝。控制台的「获取模型列表」弹窗走这里，
 * 已保存渠道也能带着草稿改动重新探测。
 */
export async function fetchUpstreamModelsBatch(params: {
  channel?: string
  channel_id?: number
  base_url?: string
  type?: number
  key?: string
  advanced_custom?: string
  header_override?: string
  proxy?: string
}): Promise<FetchModelsResponse> {
  const res = await api.post(
    '/api/channels/batch/fetch-models',
    params,
    channelActionConfig()
  )
  const models = (res.data as { models?: unknown } | undefined)?.models
  return Array.isArray(models)
    ? models.filter((model): model is string => typeof model === 'string')
    : []
}

/**
 * Delete an Ollama model from a channel
 */
export async function deleteOllamaModel(params: {
  channel_id: number
  model_name: string
}): Promise<void> {
  await api.delete(
    '/api/channel/ollama/delete',
    channelActionConfig({ data: params })
  )
}

/**
 * Test all enabled channels
 */
export async function testAllChannels(): Promise<void> {
  await api.get('/api/channel/test', channelActionConfig())
}

/**
 * Get all available models
 */
export async function getAllModels(): Promise<
  Array<{ id: string; [key: string]: unknown }>
> {
  const res = await api.get('/api/channel/models')
  return Array.isArray(res.data) ? res.data : []
}

/**
 * Get all enabled models
 */
export async function getEnabledModels(): Promise<string[]> {
  const res = await api.get('/api/channel/models_enabled')
  return Array.isArray(res.data) ? res.data : []
}

// ============================================================================
// Ollama Utilities
// ============================================================================

/**
 * Check Ollama version for a given channel
 */
export async function getOllamaVersion(
  channelId: number
): Promise<{ version: string }> {
  const res = await api.get(`/api/channel/ollama/version/${channelId}`)
  return res.data
}

// ============================================================================
// Codex Response Types
// ============================================================================

/**
 * Codex 用量/重置额度响应（api-spec §5.3.1）：
 * 成功 `{upstream_status, body}`；上游非 2xx 时后端回 502 upstream_error。
 */
export type CodexUsageResponse = {
  upstream_status: number
  body: unknown
}

export type CodexResetCreditsResponse = CodexUsageResponse

export type CodexUsageResetResponse = CodexUsageResponse

/** 刷新 Codex 凭据成功体（api-spec §5.3.1）。 */
export type CodexCredentialRefreshResponse = {
  expires_at?: string
  last_refresh?: string
  account_id?: string
  email?: string
  channel_id?: number
  channel_type?: number
  channel_name?: string
}
