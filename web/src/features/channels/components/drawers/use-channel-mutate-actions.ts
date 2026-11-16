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
import { useEffect, useCallback } from 'react'
import type { SubmitErrorHandler } from 'react-hook-form'
import { toast } from 'sonner'

import { pbrModelsQueryKey } from '@/features/routes/api'
import { handleServerError } from '@/lib/handle-server-error'

import { searchChannels } from '../../api'
import { ERROR_MESSAGES } from '../../constants'
import { useChannelMutateForm } from '../../hooks/use-channel-mutate-form'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelsQueryKeys,
  type ChannelFormValues,
  parseModelsString,
  formatModelsArray,
  hasModelConfigChanged,
  findMissingModelsInMapping,
  validateModelMappingJson,
} from '../../lib'
import { getChannelConfigurationSection } from '../../lib/channel-configuration'
import {
  collectInvalidStatusCodeEntries,
  collectNewDisallowedStatusCodeRedirects,
} from '../../lib/status-code-risk-guard'
import type { MissingModelsAction } from '../dialogs/missing-models-confirmation-dialog'
import {
  type ChannelMutateDialogProps,
  SENSITIVE_FORM_FIELDS,
} from './channel-mutate-dialog-model'
import type { ChannelMutateState } from './use-channel-mutate-state'
/* eslint-disable react-hooks/exhaustive-deps -- setters/refs come from the controller object (stable across renders). */

export function useChannelMutateActions(
  state: ChannelMutateState,
  props: ChannelMutateDialogProps
) {
  const { currentRow, onOpenChange } = props
  const {
    canEditSensitive,
    channelData,
    channelFormRef,
    channelId,
    configurationSection,
    copyToClipboard,
    currentModelsArray,
    form,
    initialModelMappingRef,
    initialModelsRef,
    initialStatusCodeMappingRef,
    isEditing,
    isMultiKeyChannel,
    missingModelsResolveRef,
    newModelDraft,
    pendingErrorFocus,
    queryClient,
    referencedModelsResolveRef,
    sensitiveLocked,
    setClearModelsConfirmOpen,
    setClipboardConnectionInfo,
    setConfigurationSection,
    setDiscardOpen,
    setMissingModelsDialogOpen,
    setMissingModelsList,
    setNewModelDraft,
    setOpen,
    setPendingErrorFocus,
    setReferencedModelsDialogOpen,
    setReferencedModelsLanes,
    setStatusCodeRiskDetailItems,
    setStatusCodeRiskOpen,
    statusCodeRiskResolveRef,
    t,
    updateModels,
  } = state
  // Handle model operations
  const handleClearModels = useCallback(() => {
    setClearModelsConfirmOpen(true)
  }, [])

  const handleConfirmClearModels = useCallback(() => {
    form.setValue('models', '')
    setClearModelsConfirmOpen(false)
    toast.success(t('Draft cleared. Save the channel to apply the change.'))
  }, [form, t])

  const handleCopyModels = useCallback(async () => {
    const models = form.getValues('models')
    if (!models?.trim()) {
      toast.info(t('No models to copy'))
      return
    }
    await copyToClipboard(models)
  }, [form, copyToClipboard, t])

  const handleAddManualModel = useCallback(
    (raw?: string) => {
      // 支持一次输入/粘贴多个模型名（逗号、顿号、空白或换行分隔），例如直接从上游
      // 文档复制一列；只输入一个时行为不变，回车即添加。粘贴走 raw（单行 input 会
      // 丢掉换行，必须在 onPaste 里先取剪贴板原文）。
      const parsed = (raw ?? newModelDraft)
        .split(/[\s,，、\n]+/)
        .map((item) => item.trim())
        .filter(Boolean)
      if (parsed.length === 0) return
      const known = new Set(currentModelsArray)
      const additions: string[] = []
      for (const model of parsed) {
        if (known.has(model)) continue
        known.add(model)
        additions.push(model)
      }
      if (additions.length === 0) {
        toast.info(t('Model already exists'))
      } else {
        updateModels([...currentModelsArray, ...additions])
        if (additions.length > 1) {
          toast.success(
            t('Added {{count}} models', { count: additions.length })
          )
        }
      }
      setNewModelDraft('')
    },
    [currentModelsArray, newModelDraft, t, updateModels]
  )

  const handleRemoveModel = useCallback(
    (model: string) => {
      updateModels(currentModelsArray.filter((item) => item !== model))
    },
    [currentModelsArray, updateModels]
  )

  const handleModelDiscoveryApply = useCallback(
    (selected: string[]) => {
      updateModels(selected)
    },
    [updateModels]
  )

  // Handle successful submission
  const handleSuccess = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
    if (channelId) {
      queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.detail(channelId),
      })
    }
    // 渠道收窄模型可能清理车道成员/删除空车道，路由页与编排器目录必须立即失效。
    queryClient.invalidateQueries({ queryKey: pbrModelsQueryKey })
    queryClient.invalidateQueries({ queryKey: ['pbr-channel-catalog'] })
    queryClient.invalidateQueries({ queryKey: ['pbr-route'] })
    onOpenChange(false)
    setOpen(null)
  }, [channelId, queryClient, onOpenChange, setOpen])

  // Show missing models confirmation dialog
  const confirmMissingModelMappings = useCallback(
    (missingModels: string[]): Promise<MissingModelsAction> => {
      return new Promise((resolve) => {
        setMissingModelsList(missingModels)
        setMissingModelsDialogOpen(true)
        missingModelsResolveRef.current = resolve
      })
    },
    []
  )

  // Handle missing models dialog action
  const handleMissingModelsAction = useCallback(
    (action: MissingModelsAction) => {
      setMissingModelsDialogOpen(false)
      if (missingModelsResolveRef.current) {
        missingModelsResolveRef.current(action)
        missingModelsResolveRef.current = null
      }
    },
    []
  )

  // 移除的模型仍被车道引用时，弹确认框；确认后由 hook 以 cleanup_models:true 重试。
  const confirmReferencedModels = useCallback(
    (lanes: string[]): Promise<boolean> =>
      new Promise((resolve) => {
        referencedModelsResolveRef.current = resolve
        setReferencedModelsLanes(lanes)
        setReferencedModelsDialogOpen(true)
      }),
    []
  )

  const handleReferencedModelsAction = useCallback((confirmed: boolean) => {
    setReferencedModelsDialogOpen(false)
    setReferencedModelsLanes([])
    if (referencedModelsResolveRef.current) {
      referencedModelsResolveRef.current(confirmed)
      referencedModelsResolveRef.current = null
    }
  }, [])

  const confirmStatusCodeRisk = useCallback(
    (detailItems: string[]): Promise<boolean> =>
      new Promise((resolve) => {
        statusCodeRiskResolveRef.current = resolve
        setStatusCodeRiskDetailItems(detailItems)
        setStatusCodeRiskOpen(true)
      }),
    []
  )

  const handleStatusCodeRiskAction = useCallback((confirmed: boolean) => {
    setStatusCodeRiskOpen(false)
    setStatusCodeRiskDetailItems([])
    if (statusCodeRiskResolveRef.current) {
      statusCodeRiskResolveRef.current(confirmed)
      statusCodeRiskResolveRef.current = null
    }
  }, [])

  useEffect(() => {
    return () => {
      if (statusCodeRiskResolveRef.current) {
        statusCodeRiskResolveRef.current(false)
        statusCodeRiskResolveRef.current = null
      }
      if (referencedModelsResolveRef.current) {
        referencedModelsResolveRef.current(false)
        referencedModelsResolveRef.current = null
      }
    }
  }, [])

  const channelMutation = useChannelMutateForm({
    currentRow,
    isEditing,
    isMultiKeyChannel,
    onSuccess: handleSuccess,
    onReferencedModels: confirmReferencedModels,
  })

  const isSubmitting = channelMutation.isPending || form.formState.isSubmitting

  // Submit handler
  const onSubmit = useCallback(
    async (data: ChannelFormValues) => {
      if (isEditing && !channelData) return
      if (!isEditing && !canEditSensitive) return
      // Validate key is required when creating
      if (!isEditing && !data.key?.trim()) {
        form.setError('key', {
          type: 'manual',
          message: ERROR_MESSAGES.REQUIRED_KEY,
        })
        setConfigurationSection('connection')
        setPendingErrorFocus('key')
        return
      }

      if (sensitiveLocked) {
        const dirtyFields = form.formState.dirtyFields as Partial<
          Record<keyof ChannelFormValues, unknown>
        >
        const hasSensitiveChanges = SENSITIVE_FORM_FIELDS.some((field) =>
          Boolean(dirtyFields[field])
        )
        if (hasSensitiveChanges) {
          toast.error(
            t('You do not have permission to edit sensitive channel settings.')
          )
          return
        }
      }

      // 渠道名是全局唯一寻址键：在提交前先做一次精确重名检查，
      // 让用户在表单里直接看到字段错误；后端仍会兜底返回 409。
      const trimmedName = data.name.trim()
      if (trimmedName) {
        try {
          const existing = await searchChannels({
            keyword: trimmedName,
            p: 1,
            page_size: 50,
          })
          const duplicate = (existing.items ?? []).find(
            (channel) =>
              channel.name.trim() === trimmedName &&
              (!isEditing || channel.id !== channelData?.id)
          )
          if (duplicate) {
            const message = t(
              'A channel with this name already exists. Channel names must be unique.'
            )
            form.setError('name', { type: 'manual', message })
            setConfigurationSection('connection')
            setPendingErrorFocus('name')
            toast.error(message)
            return
          }
        } catch {
          // 查询失败不阻断保存：后端仍会做唯一性校验并返回可判定错误。
        }
      }

      // Validate status_code_mapping entries
      if (data.status_code_mapping?.trim()) {
        const invalidEntries = collectInvalidStatusCodeEntries(
          data.status_code_mapping
        )
        if (invalidEntries.length > 0) {
          const message = t(
            'Invalid status code mapping entries: {{entries}}',
            {
              entries: invalidEntries.join(', '),
            }
          )
          form.setError('status_code_mapping', { type: 'manual', message })
          setConfigurationSection('other')
          setPendingErrorFocus('status_code_mapping')
          toast.error(message)
          return
        }

        const riskyRedirects = collectNewDisallowedStatusCodeRedirects(
          initialStatusCodeMappingRef.current,
          data.status_code_mapping
        )
        if (riskyRedirects.length > 0) {
          const confirmed = await confirmStatusCodeRisk(riskyRedirects)
          if (!confirmed) return
        }
      }

      // Validate model_mapping JSON format
      const hasModelMapping =
        typeof data.model_mapping === 'string' &&
        data.model_mapping.trim() !== ''
      const modelMappingValue = data.model_mapping || ''

      if (hasModelMapping) {
        const validation = validateModelMappingJson(modelMappingValue)
        if (!validation.valid) {
          form.setError('model_mapping', {
            type: 'manual',
            message: t(validation.error || 'Invalid model mapping'),
          })
          setConfigurationSection('routing')
          setPendingErrorFocus('model_mapping')
          handleServerError(
            validation,
            t(validation.error || 'Invalid model mapping')
          )
          return
        }
      }

      // Normalize models array
      const normalizedModels = parseModelsString(data.models || '')

      // Check for missing models in model_mapping
      if (hasModelMapping) {
        const missingModels = findMissingModelsInMapping(
          modelMappingValue,
          normalizedModels
        )

        const shouldPromptMissing =
          missingModels.length > 0 &&
          hasModelConfigChanged(
            normalizedModels,
            data.model_mapping || '',
            initialModelsRef.current,
            initialModelMappingRef.current
          )

        if (shouldPromptMissing) {
          const confirmAction = await confirmMissingModelMappings(missingModels)
          if (confirmAction === 'cancel') {
            return
          }
          if (confirmAction === 'add') {
            const updatedModels = [
              ...new Set([...normalizedModels, ...missingModels]),
            ]
            data.models = formatModelsArray(updatedModels)
            form.setValue('models', data.models)
          }
        }
      }

      try {
        await channelMutation.mutateAsync(data)
      } catch {
        // The mutation reports the server error; keep the draft open for correction.
      }
    },
    [
      isEditing,
      canEditSensitive,
      sensitiveLocked,
      channelData,
      form,
      confirmMissingModelMappings,
      confirmStatusCodeRisk,
      channelMutation,
      t,
    ]
  )

  const onInvalid: SubmitErrorHandler<ChannelFormValues> = useCallback(
    (errors) => {
      const field = Object.keys(errors)[0]
      if (field) {
        setConfigurationSection(getChannelConfigurationSection(field))
        setPendingErrorFocus(field)
      }
      toast.error(t('Please fix the highlighted fields before saving'))
    },
    [t]
  )

  useEffect(() => {
    if (!pendingErrorFocus) return
    const frame = window.requestAnimationFrame(() => {
      const panel = channelFormRef.current?.querySelector(
        '[role="tabpanel"]:not([hidden])'
      )
      const invalid = panel?.querySelector<HTMLElement>('[aria-invalid="true"]')
      const item = invalid?.closest<HTMLElement>('[data-slot="form-item"]')
      const controls =
        'input:not([type="hidden"]), textarea, button, [contenteditable="true"], [tabindex]:not([tabindex="-1"])'
      const focusTarget = invalid?.matches(controls)
        ? invalid
        : item?.querySelector<HTMLElement>(controls)
      item?.scrollIntoView({ block: 'center', behavior: 'smooth' })
      focusTarget?.focus({ preventScroll: true })
      setPendingErrorFocus(null)
    })
    return () => window.cancelAnimationFrame(frame)
  }, [pendingErrorFocus, configurationSection])

  // Handle drawer close
  const handleOpenChange = useCallback(
    (v: boolean) => {
      if (!v && isSubmitting) return
      if (!v && form.formState.isDirty) {
        setDiscardOpen(true)
        return
      }
      onOpenChange(v)
      if (!v) {
        form.reset(CHANNEL_FORM_DEFAULT_VALUES)
        setClipboardConnectionInfo(null)
      }
    },
    [onOpenChange, form, isSubmitting]
  )

  return {
    handleClearModels,
    handleConfirmClearModels,
    handleCopyModels,
    handleAddManualModel,
    handleRemoveModel,
    handleModelDiscoveryApply,
    handleSuccess,
    handleMissingModelsAction,
    handleReferencedModelsAction,
    handleStatusCodeRiskAction,
    channelMutation,
    isSubmitting,
    onSubmit,
    onInvalid,
    handleOpenChange,
  }
}

export type ChannelMutateActions = ReturnType<typeof useChannelMutateActions>
