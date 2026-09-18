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
import type { QueryClient } from '@tanstack/react-query'
import i18next from 'i18next'
import { toast } from 'sonner'

import { pbrModelsQueryKey } from '@/features/routes/api'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  copyChannel,
  deleteChannel,
  testChannel,
  updateChannelStatus,
  batchUpdateChannelStatus,
  batchDeleteChannels,
  batchSetChannelTag,
  enableTagChannels,
  disableTagChannels,
  deleteDisabledChannels,
  fixChannelAbilities,
  testAllChannels,
} from '../api'
import { CHANNEL_STATUS, ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import type { ChannelTestResponse, CopyChannelParams } from '../types'
import {
  parseChannelReferenceConflict,
  type ChannelReferenceConflict,
} from './channel-reference-conflict'

// ============================================================================
// Query Keys
// ============================================================================

export const channelsQueryKeys = {
  all: ['channels'] as const,
  defaultBaseURLs: () =>
    [...channelsQueryKeys.all, 'default_base_urls'] as const,
  lists: () => [...channelsQueryKeys.all, 'list'] as const,
  list: (params: Record<string, unknown>) =>
    [...channelsQueryKeys.lists(), params] as const,
  details: () => [...channelsQueryKeys.all, 'detail'] as const,
  detail: (id: number) => [...channelsQueryKeys.details(), id] as const,
}

/** 删除/批量删除被车道引用时的冲突信息，供确认框内联展示与跳转处理。 */
export type ChannelActionFailure = ChannelReferenceConflict

/** 把统一解析结果映射成本模块的失败描述符（details 权威，message 仅兜底）。 */
function channelReferenceFailure(
  error: unknown,
  fallbackMessage: string
): ChannelActionFailure {
  return parseChannelReferenceConflict(error, fallbackMessage)
}

/**
 * 渠道写操作成功后统一失效缓存：渠道列表之外，路由页的 pbr-routable-models
 * 也依赖渠道成员/状态，必须一起失效，否则路由与故障切换仍显示旧的可调用状态。
 */
function invalidateChannelCaches(queryClient?: QueryClient): void {
  queryClient?.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
  queryClient?.invalidateQueries({ queryKey: pbrModelsQueryKey })
}

function getChannelTestResponseTime(
  response: ChannelTestResponse
): number | undefined {
  const responseTime = response.data?.response_time
  if (typeof responseTime === 'number' && Number.isFinite(responseTime)) {
    return responseTime
  }

  if (
    typeof response.time === 'number' &&
    Number.isFinite(response.time) &&
    response.time > 0
  ) {
    return Math.round(response.time * 1000)
  }

  return undefined
}

function formatChannelTestDuration(responseTime?: number): string | undefined {
  if (responseTime === undefined) return undefined

  if (responseTime >= 1000) {
    return `${(responseTime / 1000).toFixed(2)} s`
  }

  return `${Math.max(1, Math.round(responseTime))} ms`
}

function getChannelTestLabel(options?: {
  channelName?: string
  testModel?: string
}): string {
  const channelName = options?.channelName?.trim()
  const testModel = options?.testModel?.trim()

  if (channelName && testModel) {
    return i18next.t('Channel {{name}} model {{model}}', {
      name: channelName,
      model: testModel,
    })
  }

  if (channelName) {
    return i18next.t('Channel {{name}}', { name: channelName })
  }

  if (testModel) {
    return i18next.t('Model {{model}}', { model: testModel })
  }

  return i18next.t('Channel')
}

// ============================================================================
// Single Channel Actions
// ============================================================================

/**
 * Enable a channel.
 *
 * 新契约下非 2xx 会被 axios 拒绝，因此走到这里就代表成功。
 */
export async function handleEnableChannel(
  id: number,
  queryClient?: QueryClient,
  onSuccess?: () => void
): Promise<void> {
  try {
    await updateChannelStatus(id, CHANNEL_STATUS.ENABLED)
    toast.success(i18next.t(SUCCESS_MESSAGES.ENABLED))
    invalidateChannelCaches(queryClient)
    onSuccess?.()
  } catch (error) {
    handleServerError(error, i18next.t(ERROR_MESSAGES.UPDATE_FAILED))
  }
}

/**
 * Disable a channel
 */
export async function handleDisableChannel(
  id: number,
  queryClient?: QueryClient,
  onSuccess?: () => void
): Promise<void> {
  try {
    await updateChannelStatus(id, CHANNEL_STATUS.MANUAL_DISABLED)
    toast.success(i18next.t(SUCCESS_MESSAGES.DISABLED))
    invalidateChannelCaches(queryClient)
    onSuccess?.()
  } catch (error) {
    handleServerError(error, i18next.t(ERROR_MESSAGES.UPDATE_FAILED))
  }
}

/**
 * Toggle channel status (enable/disable)
 */
export async function handleToggleChannelStatus(
  id: number,
  currentStatus: number,
  queryClient?: QueryClient,
  onSuccess?: () => void
): Promise<void> {
  if (currentStatus === CHANNEL_STATUS.ENABLED) {
    await handleDisableChannel(id, queryClient, onSuccess)
  } else {
    await handleEnableChannel(id, queryClient, onSuccess)
  }
}

/**
 * Delete a channel.
 *
 * 成功返回 null（已提示并失效缓存）；被车道引用等失败时返回描述符，调用方据此
 * 保持确认框打开并展示引用车道清单，只有成功才关闭。
 */
export async function handleDeleteChannel(
  id: number,
  queryClient?: QueryClient,
  onSuccess?: () => void
): Promise<ChannelActionFailure | null> {
  try {
    await deleteChannel(id)
    toast.success(i18next.t(SUCCESS_MESSAGES.DELETED))
    invalidateChannelCaches(queryClient)
    onSuccess?.()
    return null
  } catch (error) {
    return channelReferenceFailure(
      error,
      i18next.t(ERROR_MESSAGES.DELETE_FAILED)
    )
  }
}

/**
 * Test channel connectivity
 */
export async function handleTestChannel(
  id: number,
  options?: {
    channelName?: string
    testModel?: string
    endpointType?: string
    stream?: boolean
    silent?: boolean
  },
  onTestComplete?: (
    success: boolean,
    responseTime?: number,
    error?: string,
    errorCode?: string
  ) => void
): Promise<void> {
  const payload =
    options && (options.testModel || options.endpointType || options.stream)
      ? {
          ...(options.testModel ? { model: options.testModel } : {}),
          ...(options.endpointType
            ? { endpoint_type: options.endpointType }
            : {}),
          ...(options.stream ? { stream: true } : {}),
        }
      : undefined

  try {
    const response = await testChannel(id, payload)
    const responseTime = getChannelTestResponseTime(response)
    const duration = formatChannelTestDuration(responseTime)
    const target = getChannelTestLabel(options)
    // 探活结果仍是 200 + `success:false`（探活失败是"测试结论"，不是请求失败）。
    if (response.success) {
      if (!options?.silent) {
        toast.success(
          i18next.t('{{target}} test succeeded', { target }),
          duration
            ? {
                description: i18next.t('Response time: {{duration}}', {
                  duration,
                }),
              }
            : undefined
        )
      }
      onTestComplete?.(true, responseTime)
    } else {
      const errorMsg = response.message || i18next.t(ERROR_MESSAGES.TEST_FAILED)
      if (!options?.silent) {
        handleServerError(response, undefined, {
          title: i18next.t('{{target}} test failed', { target }),
          description: response.error_code
            ? `${errorMsg} (${response.error_code})`
            : errorMsg,
        })
      }
      onTestComplete?.(false, responseTime, errorMsg, response.error_code)
    }
  } catch (error: unknown) {
    const errorMsg =
      getServerErrorMessage(error) || i18next.t(ERROR_MESSAGES.TEST_FAILED)
    const target = getChannelTestLabel(options)
    if (!options?.silent) {
      handleServerError(error, undefined, {
        title: i18next.t('{{target}} test failed', { target }),
        description: errorMsg,
      })
    }
    onTestComplete?.(false, undefined, errorMsg)
  }
}

/**
 * Copy a channel
 */
export async function handleCopyChannel(
  id: number,
  params: CopyChannelParams,
  queryClient?: QueryClient,
  onSuccess?: (newId: number) => void
): Promise<void> {
  try {
    const response = await copyChannel(id, params)
    toast.success(i18next.t(SUCCESS_MESSAGES.COPIED))
    invalidateChannelCaches(queryClient)
    onSuccess?.(response.id ?? 0)
  } catch (error) {
    handleServerError(error, i18next.t('Failed to copy channel'))
  }
}

// ============================================================================
// Batch Actions
// ============================================================================

/**
 * Batch delete channels
 */
export async function handleBatchDelete(
  ids: number[],
  queryClient?: QueryClient,
  onSuccess?: (deletedCount: number) => void
): Promise<ChannelActionFailure | null> {
  if (ids.length === 0) {
    toast.error(i18next.t('No channels selected'))
    return null
  }

  try {
    const deletedCount = await batchDeleteChannels({ ids })
    toast.success(
      i18next.t('{{count}} channel(s) deleted', { count: deletedCount })
    )
    invalidateChannelCaches(queryClient)
    onSuccess?.(deletedCount)
    return null
  } catch (error) {
    return channelReferenceFailure(
      error,
      i18next.t(ERROR_MESSAGES.DELETE_FAILED)
    )
  }
}

/**
 * Batch enable channels
 */
export async function handleBatchEnable(
  ids: number[],
  queryClient?: QueryClient,
  onSuccess?: () => void
): Promise<boolean> {
  if (ids.length === 0) {
    toast.error(i18next.t('No channels selected'))
    return false
  }

  try {
    const changed = await batchUpdateChannelStatus(ids, CHANNEL_STATUS.ENABLED)
    const successCount = changed > 0 ? changed : ids.length
    toast.success(
      i18next.t('{{count}} channel(s) enabled', { count: successCount })
    )
    invalidateChannelCaches(queryClient)
    onSuccess?.()
    return true
  } catch (error) {
    handleServerError(error, i18next.t('Failed to enable channels'))
    return false
  }
}

/**
 * Batch disable channels
 */
export async function handleBatchDisable(
  ids: number[],
  queryClient?: QueryClient,
  onSuccess?: () => void
): Promise<boolean> {
  if (ids.length === 0) {
    toast.error(i18next.t('No channels selected'))
    return false
  }

  try {
    const changed = await batchUpdateChannelStatus(
      ids,
      CHANNEL_STATUS.MANUAL_DISABLED
    )
    const successCount = changed > 0 ? changed : ids.length
    toast.success(
      i18next.t('{{count}} channel(s) disabled', { count: successCount })
    )
    invalidateChannelCaches(queryClient)
    onSuccess?.()
    return true
  } catch (error) {
    handleServerError(error, i18next.t('Failed to disable channels'))
    return false
  }
}

/**
 * Batch set tag
 */
export async function handleBatchSetTag(
  ids: number[],
  tag: string | null,
  queryClient?: QueryClient,
  onSuccess?: () => void
): Promise<void> {
  if (ids.length === 0) {
    toast.error(i18next.t('No channels selected'))
    return
  }

  try {
    await batchSetChannelTag({ ids, tag })
    toast.success(i18next.t(SUCCESS_MESSAGES.TAG_SET))
    invalidateChannelCaches(queryClient)
    onSuccess?.()
  } catch (error) {
    handleServerError(error, i18next.t('Failed to set tag'))
  }
}

// ============================================================================
// Tag-Based Actions
// ============================================================================

/**
 * Enable all channels with a tag
 */
export async function handleEnableTagChannels(
  tag: string,
  queryClient?: QueryClient,
  onSuccess?: () => void
): Promise<void> {
  try {
    await enableTagChannels(tag)
    toast.success(i18next.t('Enabled all channels with tag: {{tag}}', { tag }))
    invalidateChannelCaches(queryClient)
    onSuccess?.()
  } catch (error) {
    handleServerError(error, i18next.t('Failed to enable tag channels'))
  }
}

/**
 * Disable all channels with a tag
 */
export async function handleDisableTagChannels(
  tag: string,
  queryClient?: QueryClient,
  onSuccess?: () => void
): Promise<void> {
  try {
    await disableTagChannels(tag)
    toast.success(i18next.t('Disabled all channels with tag: {{tag}}', { tag }))
    invalidateChannelCaches(queryClient)
    onSuccess?.()
  } catch (error) {
    handleServerError(error, i18next.t('Failed to disable tag channels'))
  }
}

// ============================================================================
// System Actions
// ============================================================================

/**
 * Delete all disabled channels
 */
export async function handleDeleteAllDisabled(
  queryClient?: QueryClient,
  onSuccess?: (deletedCount: number) => void
): Promise<ChannelActionFailure | null> {
  try {
    const deletedCount = await deleteDisabledChannels()
    toast.success(
      i18next.t('{{count}} disabled channel(s) deleted', {
        count: deletedCount,
      })
    )
    invalidateChannelCaches(queryClient)
    onSuccess?.(deletedCount)
    return null
  } catch (error) {
    return channelReferenceFailure(
      error,
      i18next.t('Failed to delete disabled channels')
    )
  }
}

/**
 * Repair channel consistency
 *
 * 稳定面成功体是 `{repaired,failed}`；基座面 `POST /api/channel/fix` 仍是
 * `{success,fails}` 的裸化。两者都接受。
 */
export async function handleFixAbilities(
  queryClient?: QueryClient,
  onSuccess?: (result: { success: number; fails: number }) => void
): Promise<void> {
  try {
    const response = await fixChannelAbilities()
    const raw = response as {
      repaired?: number
      failed?: number
      success?: number
      fails?: number
    }
    const result = {
      success: raw.repaired ?? raw.success ?? 0,
      fails: raw.failed ?? raw.fails ?? 0,
    }
    toast.success(
      i18next.t(
        'Channel consistency repaired: {{success}} succeeded, {{fails}} failed',
        result
      )
    )
    invalidateChannelCaches(queryClient)
    onSuccess?.(result)
  } catch (error) {
    handleServerError(error, i18next.t('Failed to repair channel consistency'))
  }
}

/**
 * Test all enabled channels
 */
export async function handleTestAllChannels(
  queryClient?: QueryClient,
  onSuccess?: () => void
): Promise<void> {
  try {
    await testAllChannels()
    toast.success(
      i18next.t(
        'Testing all enabled channels started. Please refresh to see results.'
      )
    )
    invalidateChannelCaches(queryClient)
    onSuccess?.()
  } catch (error) {
    handleServerError(error, i18next.t('Failed to test all channels'))
  }
}
