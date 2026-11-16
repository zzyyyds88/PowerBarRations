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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState, useCallback, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { useHiddenClickUnlock } from '@/hooks/use-hidden-click-unlock'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import {
  parseChannelConnectionInfo,
  type ChannelConnectionInfo,
} from '@/lib/channel-connection-info'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import {
  getChannel,
  getChannelDefaultBaseURLs,
  getChannelKeyByName,
} from '../../api'
import { CHANNEL_PROTOCOL_OPTIONS, FIELD_PLACEHOLDERS } from '../../constants'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  channelsQueryKeys,
  parseModelsString,
  transformChannelToFormDefaults,
  type ChannelFormValues,
} from '../../lib'
import type { ChannelConfigurationSection } from '../../lib/channel-configuration'
import type { ProviderPreset } from '../../lib/provider-presets'
import { useChannels } from '../channels-provider'
import type { MissingModelsAction } from '../dialogs/missing-models-confirmation-dialog'
import type { ChannelMutateDialogProps } from './channel-mutate-dialog-model'

export function useChannelMutateBaseState(props: ChannelMutateDialogProps) {
  const { currentRow, open } = props
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { setOpen } = useChannels()
  const currentUser = useAuthStore((s) => s.auth.user)
  const canEditSensitive = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
  )
  const canOperateChannel = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.OPERATE
  )
  const [isCodexCredentialRefreshing, setIsCodexCredentialRefreshing] =
    useState(false)
  const initialModelsRef = useRef<string[]>([])
  const initialModelMappingRef = useRef<string>('')
  const initialStatusCodeMappingRef = useRef<string>('')
  const [statusCodeRiskOpen, setStatusCodeRiskOpen] = useState(false)
  const [statusCodeRiskDetailItems, setStatusCodeRiskDetailItems] = useState<
    string[]
  >([])
  const statusCodeRiskResolveRef = useRef<
    ((confirmed: boolean) => void) | null
  >(null)
  const [missingModelsDialogOpen, setMissingModelsDialogOpen] = useState(false)
  const [missingModelsList, setMissingModelsList] = useState<string[]>([])
  const missingModelsResolveRef = useRef<
    ((action: MissingModelsAction) => void) | null
  >(null)
  const [referencedModelsDialogOpen, setReferencedModelsDialogOpen] =
    useState(false)
  const [referencedModelsLanes, setReferencedModelsLanes] = useState<string[]>(
    []
  )
  const referencedModelsResolveRef = useRef<
    ((confirmed: boolean) => void) | null
  >(null)
  const [clearModelsConfirmOpen, setClearModelsConfirmOpen] = useState(false)
  const [discardOpen, setDiscardOpen] = useState(false)
  const channelFormRef = useRef<HTMLFormElement>(null)
  const [modelDiscoveryDialogOpen, setModelDiscoveryDialogOpen] =
    useState(false)
  const [pendingDiscoveryOpen, setPendingDiscoveryOpen] = useState(false)
  const [newModelDraft, setNewModelDraft] = useState('')
  const [paramOverrideEditorOpen, setParamOverrideEditorOpen] = useState(false)
  const [advancedCustomEditorOpen, setAdvancedCustomEditorOpen] =
    useState(false)
  const [clipboardConnectionInfo, setClipboardConnectionInfo] =
    useState<ChannelConnectionInfo | null>(null)

  const isEditing = Boolean(currentRow)
  const channelId = currentRow?.id ?? null
  const sensitiveLocked = isEditing && !canEditSensitive
  const [configurationSection, setConfigurationSection] =
    useState<ChannelConfigurationSection>('connection')
  const [pendingErrorFocus, setPendingErrorFocus] = useState<string | null>(
    null
  )
  const loadedForm = useRef<{ channelId: number; snapshot: string } | null>(
    null
  )

  const { data: defaultBaseURLs } = useQuery({
    queryKey: channelsQueryKeys.defaultBaseURLs(),
    // Optional hints must not trigger the global error-page redirect.
    queryFn: () => getChannelDefaultBaseURLs().catch(() => null),
    enabled: open,
    staleTime: 5 * 60 * 1000,
  })

  // Fetch channel details if editing
  const {
    data: channelData,
    isLoading: isChannelLoading,
    isError: isChannelError,
    error: channelError,
    refetch: refetchChannel,
  } = useQuery({
    queryKey: channelsQueryKeys.detail(channelId || 0),
    queryFn: async () => getChannel(channelId || 0),
    enabled: open && isEditing && Boolean(channelId),
    meta: { errorToast: false },
  })

  const { copyToClipboard } = useCopyToClipboard()

  // Check if this is a multi-key channel
  const isMultiKeyChannel =
    isEditing && channelData?.channel_info?.is_multi_key === true

  // 渲染期读取的初始模型清单：与写入 ref 的 effect 同源，但由数据直接派生，
  // 避免在 render 中访问 ref.current（react/refs）。
  const initialModels = useMemo(
    () => (isEditing ? parseModelsString(channelData?.models || '') : []),
    [isEditing, channelData?.models]
  )

  // Form setup
  const form = useForm<ChannelFormValues>({
    resolver: zodResolver(channelFormSchema),
    defaultValues: CHANNEL_FORM_DEFAULT_VALUES,
  })

  // Watch values once for conditional fields and configuration indicators.
  const formValues = form.watch()
  const multiKeyMode = formValues.multi_key_mode
  const currentType = formValues.type
  const baseUrlPlaceholder =
    defaultBaseURLs?.[currentType] || t(FIELD_PLACEHOLDERS.BASE_URL)
  const currentStatus = formValues.status
  const currentBaseUrl = formValues.base_url
  const currentKey = formValues.key
  const currentModels = formValues.models
  // 内置服务商预设：只填协议 / 地址 / 名称。硬性请求头由网关按目标 host 自动
  // 补齐（relay/channel/provider_quirks.go），不写进渠道配置，用户无需关心。
  const applyProviderPreset = useCallback(
    (preset: ProviderPreset) => {
      if (!canEditSensitive) return
      const option = CHANNEL_PROTOCOL_OPTIONS.find(
        (item) => item.value === preset.protocol
      )
      if (option) {
        form.setValue('type', option.type, { shouldDirty: true })
        form.setValue('protocol', preset.protocol, { shouldDirty: true })
      }
      form.setValue('base_url', preset.baseUrl, { shouldDirty: true })
      if (!form.getValues('name').trim()) {
        form.setValue('name', preset.name, { shouldDirty: true })
      }
    },
    [canEditSensitive, form]
  )
  const currentModelMapping = formValues.model_mapping
  const awsKeyType = formValues.aws_key_type
  const vertexKeyType = formValues.vertex_key_type
  const upstreamModelUpdateCheckEnabled =
    formValues.upstream_model_update_check_enabled
  const currentSettings = formValues.settings
  const currentAdvancedCustom = formValues.advanced_custom
  const currentHeaderOverride = formValues.header_override
  const currentProxy = formValues.proxy
  const currentHttpProtocol = formValues.http_protocol
  const {
    unlocked: doubaoApiEditUnlocked,
    handleClick: handleApiConfigSecretClick,
    reset: resetDoubaoApiUnlock,
  } = useHiddenClickUnlock({
    requiredClicks: 10,
    disabled: currentType !== 45 || sensitiveLocked,
    onUnlock: () => {
      toast.info(t('Doubao custom API address editing unlocked'))
    },
  })

  useEffect(() => {
    if (!open) {
      resetDoubaoApiUnlock()
    }
  }, [open, resetDoubaoApiUnlock])

  const applyConnectionInfo = useCallback(
    (connectionInfo: ChannelConnectionInfo) => {
      form.setValue('key', connectionInfo.key, {
        shouldDirty: true,
        shouldValidate: true,
      })
      form.setValue('base_url', connectionInfo.url, {
        shouldDirty: true,
        shouldValidate: true,
      })
      setClipboardConnectionInfo(null)
      toast.success(t('Connection info filled in'))
    },
    [form, t]
  )

  const pasteConnectionInfoFromClipboard = useCallback(async () => {
    if (typeof navigator === 'undefined' || !navigator.clipboard?.readText) {
      toast.error(t('Unable to read clipboard'))
      return
    }

    try {
      const text = await navigator.clipboard.readText()
      const parsed = parseChannelConnectionInfo(text)
      if (parsed) {
        applyConnectionInfo(parsed)
        return
      }
      toast.info(t('No connection info found in clipboard'))
    } catch {
      toast.error(t('Unable to read clipboard'))
    }
  }, [applyConnectionInfo, t])

  useEffect(() => {
    if (!open || isEditing) {
      setClipboardConnectionInfo(null)
      return
    }

    if (typeof navigator === 'undefined' || !navigator.clipboard?.readText) {
      return
    }

    let cancelled = false
    void navigator.clipboard
      .readText()
      .then((text) => {
        if (cancelled) return
        setClipboardConnectionInfo(parseChannelConnectionInfo(text))
      })
      .catch(() => {
        /* Clipboard detection is best-effort on drawer open. */
      })

    return () => {
      cancelled = true
    }
  }, [isEditing, open])

  // Helper computed values
  const isBatchMode =
    multiKeyMode === 'batch' || multiKeyMode === 'multi_to_single'
  const isChannelDetailLoading = isEditing && isChannelLoading
  const [showKey, setShowKey] = useState(false)

  const revealKey = async () => {
    if (!showKey && isEditing && !form.getValues('key')?.trim()) {
      const name = form.getValues('name')?.trim()
      if (!name) return
      try {
        const key = await getChannelKeyByName(name)
        form.setValue('key', key)
      } catch (error) {
        handleServerError(error)
        return
      }
    }
    setShowKey((value) => !value)
  }

  // Load channel data into form when editing
  useEffect(() => {
    if (!open) {
      setModelDiscoveryDialogOpen(false)
      setPendingDiscoveryOpen(false)
      setNewModelDraft('')
      form.reset(CHANNEL_FORM_DEFAULT_VALUES)
      loadedForm.current = null
      setConfigurationSection('connection')
      setPendingErrorFocus(null)
      return
    }
    if (isEditing && channelData) {
      const isNewChannel = loadedForm.current?.channelId !== channelId
      // Model selectors also change values without setting RHF's dirty flag.
      // Refresh untouched forms, while retaining every kind of unsaved input.
      if (
        !isNewChannel &&
        loadedForm.current?.snapshot !== JSON.stringify(form.getValues())
      ) {
        return
      }
      const defaults = transformChannelToFormDefaults(channelData)
      form.reset(defaults)
      loadedForm.current = {
        channelId: channelData.id,
        snapshot: JSON.stringify(form.getValues()),
      }
      if (isNewChannel) {
        setModelDiscoveryDialogOpen(false)
        setPendingDiscoveryOpen(false)
        setNewModelDraft('')
        setConfigurationSection('connection')
        setPendingErrorFocus(null)
      }
      // Store initial values for comparison
      initialModelsRef.current = parseModelsString(channelData.models || '')
      initialModelMappingRef.current = channelData.model_mapping || ''
      initialStatusCodeMappingRef.current =
        channelData.status_code_mapping || ''
    } else if (!isEditing) {
      form.reset(CHANNEL_FORM_DEFAULT_VALUES)
      initialModelsRef.current = []
      initialModelMappingRef.current = ''
      initialStatusCodeMappingRef.current = ''
    }
  }, [isEditing, channelId, channelData, form, open])

  return {
    t,
    queryClient,
    setOpen,
    currentUser,
    canEditSensitive,
    canOperateChannel,
    isCodexCredentialRefreshing,
    setIsCodexCredentialRefreshing,
    initialModelsRef,
    initialModels,
    initialModelMappingRef,
    initialStatusCodeMappingRef,
    statusCodeRiskOpen,
    setStatusCodeRiskOpen,
    statusCodeRiskDetailItems,
    setStatusCodeRiskDetailItems,
    statusCodeRiskResolveRef,
    missingModelsDialogOpen,
    setMissingModelsDialogOpen,
    missingModelsList,
    setMissingModelsList,
    missingModelsResolveRef,
    referencedModelsDialogOpen,
    setReferencedModelsDialogOpen,
    referencedModelsLanes,
    setReferencedModelsLanes,
    referencedModelsResolveRef,
    clearModelsConfirmOpen,
    setClearModelsConfirmOpen,
    discardOpen,
    setDiscardOpen,
    channelFormRef,
    modelDiscoveryDialogOpen,
    setModelDiscoveryDialogOpen,
    pendingDiscoveryOpen,
    setPendingDiscoveryOpen,
    newModelDraft,
    setNewModelDraft,
    paramOverrideEditorOpen,
    setParamOverrideEditorOpen,
    advancedCustomEditorOpen,
    setAdvancedCustomEditorOpen,
    clipboardConnectionInfo,
    setClipboardConnectionInfo,
    isEditing,
    channelId,
    sensitiveLocked,
    configurationSection,
    setConfigurationSection,
    pendingErrorFocus,
    setPendingErrorFocus,
    loadedForm,
    defaultBaseURLs,
    channelData,
    isChannelLoading,
    isChannelError,
    channelError,
    refetchChannel,
    copyToClipboard,
    isMultiKeyChannel,
    form,
    formValues,
    multiKeyMode,
    currentType,
    baseUrlPlaceholder,
    currentStatus,
    currentBaseUrl,
    currentKey,
    currentModels,
    applyProviderPreset,
    currentModelMapping,
    awsKeyType,
    vertexKeyType,
    upstreamModelUpdateCheckEnabled,
    currentSettings,
    currentAdvancedCustom,
    currentHeaderOverride,
    currentProxy,
    currentHttpProtocol,
    applyConnectionInfo,
    pasteConnectionInfoFromClipboard,
    isBatchMode,
    isChannelDetailLoading,
    showKey,
    setShowKey,
    revealKey,
    doubaoApiEditUnlocked,
    resetDoubaoApiUnlock,
    handleApiConfigSecretClick,
    open,
  }
}

export type ChannelMutateBaseState = ReturnType<
  typeof useChannelMutateBaseState
>
