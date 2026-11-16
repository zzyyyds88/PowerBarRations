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
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { createChannel, updateChannel } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import {
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
  type ChannelFormValues,
} from '../lib'
import {
  isChannelReferenceConflict,
  parseChannelReferenceConflict,
} from '../lib/channel-reference-conflict'
import type { Channel } from '../types'

type UseChannelMutateFormParams = {
  currentRow?: Channel | null
  isEditing: boolean
  isMultiKeyChannel: boolean
  onSuccess: () => void
  /**
   * 更新时若移除了仍被车道引用的模型，后端回 409 conflict（稳定面）或
   * code=models_referenced_by_lanes（基座面）。调用方（channel-mutate-dialog）
   * 据此弹出确认框；resolve true 表示用户确认"同时从这些车道移除本渠道成员"，
   * 此时以 cleanup_models:true 重试。
   */
  onReferencedModels?: (lanes: string[]) => Promise<boolean>
}

/** mutateAsync 的结果：cancelled 表示用户在确认框里取消，调用方应保留草稿。 */
export type ChannelMutateOutcome =
  | {
      status: 'success'
      messageKey: string
      cleanedLanes?: string[]
      deletedLanes?: string[]
      manualLanesCleared?: string[]
    }
  | { status: 'cancelled' }

const SENSITIVE_UPDATE_FIELDS = [
  'type',
  'key',
  'base_url',
  'openai_organization',
  'param_override',
  'header_override',
  'setting',
  'settings',
  'other',
] satisfies (keyof Channel)[]

export function useChannelMutateForm(props: UseChannelMutateFormParams) {
  const { t } = useTranslation()
  const currentUser = useAuthStore((s) => s.auth.user)
  const canEditSensitive = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
  )

  return useMutation({
    mutationFn: async (
      data: ChannelFormValues
    ): Promise<ChannelMutateOutcome> => {
      if (props.isEditing && props.currentRow) {
        const currentRow = props.currentRow
        const payload = transformFormDataToUpdatePayload(data, currentRow.id)
        if (!data.key?.trim()) {
          delete payload.key
        }
        if (!canEditSensitive) {
          for (const field of SENSITIVE_UPDATE_FIELDS) {
            delete payload[field]
          }
        }
        // 单 key 语义：保存新 key 即整体替换，不再发送 key_mode / multi_key_mode。
        const updatePayload = payload

        // 失败一律走 axios 拒绝：先从错误包络里取车道引用明细，命中则确认后重试。
        try {
          const response = await updateChannel(currentRow.id, updatePayload)
          return {
            status: 'success',
            messageKey: SUCCESS_MESSAGES.UPDATED,
            cleanedLanes: response.cleaned_lanes ?? [],
            deletedLanes: response.deleted_lanes ?? [],
            manualLanesCleared: response.manual_lanes_cleared ?? [],
          }
        } catch (error) {
          // details 是权威来源（api-spec §3）：conflict / models_referenced_by_lanes。
          if (!isChannelReferenceConflict(error)) {
            throw error
          }
          const conflict = parseChannelReferenceConflict(error)
          const groups =
            conflict.groups.length > 0
              ? conflict.groups
              : Object.values(conflict.blockedGroups).flat()
          if (groups.length > 0) {
            throw new Error(
              t(
                'This model is still used by member groups: {{groups}}. Edit those groups first.',
                { groups: groups.join(', ') }
              )
            )
          }
          const { lanes } = conflict
          const confirmed = props.onReferencedModels
            ? await props.onReferencedModels(lanes)
            : false
          if (!confirmed) {
            // 用户取消：不报错、不保存，保留草稿与保存对话框。
            return { status: 'cancelled' }
          }
          const cleaned = await updateChannel(currentRow.id, {
            ...updatePayload,
            cleanup_models: true,
          })
          return {
            status: 'success',
            messageKey: SUCCESS_MESSAGES.UPDATED,
            cleanedLanes: cleaned.cleaned_lanes ?? [],
            deletedLanes: cleaned.deleted_lanes ?? [],
            manualLanesCleared: cleaned.manual_lanes_cleared ?? [],
          }
        }
      }

      const payload = transformFormDataToCreatePayload(data)
      await createChannel(payload)
      return { status: 'success', messageKey: SUCCESS_MESSAGES.CREATED }
    },
    onSuccess: (outcome) => {
      if (outcome.status === 'cancelled') return
      const message = t(outcome.messageKey)
      const cleanedCount = outcome.cleanedLanes?.length ?? 0
      const deletedCount = outcome.deletedLanes?.length ?? 0
      const manualLanes = outcome.manualLanesCleared ?? []
      const details: string[] = []
      if (cleanedCount > 0 || deletedCount > 0) {
        details.push(
          t(
            'Removed this channel from {{cleaned}} lane(s); deleted {{deleted}} empty lane(s). Those models are no longer callable.',
            { cleaned: cleanedCount, deleted: deletedCount }
          )
        )
      }
      if (manualLanes.length > 0) {
        details.push(
          t('Manual lanes now need an active member: {{lanes}}.', {
            lanes: manualLanes.join(', '),
          })
        )
      }
      toast.success(
        message,
        details.length > 0 ? { description: details.join(' ') } : undefined
      )
      props.onSuccess()
    },
    onError: (error: unknown) => {
      handleServerError(error, t(ERROR_MESSAGES.CREATE_FAILED))
    },
  })
}
