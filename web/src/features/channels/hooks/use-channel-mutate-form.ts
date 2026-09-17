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
import { createServerError } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { createChannel, updateChannel } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import {
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
  type ChannelFormValues,
} from '../lib'
import type { Channel } from '../types'

type UseChannelMutateFormParams = {
  currentRow?: Channel | null
  isEditing: boolean
  isMultiKeyChannel: boolean
  onSuccess: () => void
  /**
   * 更新时若移除了仍被车道引用的模型，后端返回 code=models_referenced_by_lanes。
   * 调用方（channel-mutate-dialog）据此弹出确认框；resolve true 表示用户确认
   * "同时从这些车道移除本渠道成员"，此时以 cleanup_models:true 重试。
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
        const payloadWithKeyMode =
          canEditSensitive &&
          props.isMultiKeyChannel &&
          data.key?.trim() &&
          data.key_mode
            ? {
                ...payload,
                key_mode: data.key_mode,
              }
            : payload
        const updatePayload = {
          ...payloadWithKeyMode,
          ...(canEditSensitive && props.isMultiKeyChannel
            ? { multi_key_mode: data.multi_key_type }
            : {}),
        }

        const response = await updateChannel(currentRow.id, updatePayload)
        if (
          !response.success &&
          response.code === 'models_referenced_by_lanes'
        ) {
          const lanes = response.data?.lanes ?? []
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
          if (!cleaned.success) {
            throw createServerError(cleaned, t(ERROR_MESSAGES.UPDATE_FAILED))
          }
          return {
            status: 'success',
            messageKey: SUCCESS_MESSAGES.UPDATED,
            cleanedLanes: cleaned.cleaned_lanes ?? [],
            deletedLanes: cleaned.deleted_lanes ?? [],
          }
        }
        if (!response.success) {
          throw createServerError(response, t(ERROR_MESSAGES.UPDATE_FAILED))
        }
        return { status: 'success', messageKey: SUCCESS_MESSAGES.UPDATED }
      }

      const payload = transformFormDataToCreatePayload(data)
      const response = await createChannel(payload)
      if (!response.success) {
        throw createServerError(response, t(ERROR_MESSAGES.CREATE_FAILED))
      }
      return { status: 'success', messageKey: SUCCESS_MESSAGES.CREATED }
    },
    onSuccess: (outcome) => {
      if (outcome.status === 'cancelled') return
      const message = t(outcome.messageKey)
      const cleanedCount = outcome.cleanedLanes?.length ?? 0
      const deletedCount = outcome.deletedLanes?.length ?? 0
      if (cleanedCount > 0 || deletedCount > 0) {
        toast.success(message, {
          description: t(
            'Removed this channel from {{cleaned}} lane(s); deleted {{deleted}} empty lane(s). Those models are no longer callable.',
            { cleaned: cleanedCount, deleted: deletedCount }
          ),
        })
      } else {
        toast.success(message)
      }
      props.onSuccess()
    },
    onError: (error: unknown) => {
      handleServerError(error, t(ERROR_MESSAGES.CREATE_FAILED))
    },
  })
}
