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
import { useEffect, useMemo, useCallback } from 'react'
import { toast } from 'sonner'

import { handleServerError } from '@/lib/handle-server-error'
import { getLobeIcon } from '@/lib/lobe-icon'

import { refreshCodexCredential } from '../../api'
import {
  CHANNEL_PROTOCOL_OPTIONS,
  CHANNEL_PROTOCOL_PRESENTATION,
  ERROR_MESSAGES,
  MODEL_FETCHABLE_TYPES,
} from '../../constants'
import {
  useChannelModelDiscovery,
  type ChannelModelDiscoveryRequest,
} from '../../hooks/use-channel-model-discovery'
import {
  CHANNEL_TYPE_ADVANCED_CUSTOM,
  channelsQueryKeys,
  getAdvancedCustomStats,
  getChannelProtocol,
  parseChannelOtherSettings,
  parseModelsString,
  formatModelsArray,
  extractRedirectModels,
  extractMappingSourceModels,
  normalizeModelName,
} from '../../lib'
import { getChannelConfigurationState } from '../../lib/channel-configuration'
import { PROVIDER_PRESETS } from '../../lib/provider-presets'
import type { ChannelProtocol } from '../../types'
import { ChannelTypeLogo } from '../channel-type-badge'
import {
  ADVANCED_CUSTOM_ROUTE_TYPE_PREVIEW_LIMIT,
  DISCOVERY_BASE_URL_REQUIRED_TYPES,
  MODEL_MAPPING_PREVIEW_FALLBACK,
  UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT,
  createEmptyModelMappingGuardrail,
  parseSettingsRecord,
  type ModelMappingGuardrail,
} from './channel-mutate-dialog-model'
import type { ChannelMutateBaseState } from './use-channel-mutate-base-state'
/* eslint-disable react-hooks/exhaustive-deps -- setters/refs come from the controller object (stable across renders). */

export function useChannelMutateDerivedState(state: ChannelMutateBaseState) {
  const {
    canEditSensitive,
    canOperateChannel,
    channelData,
    channelId,
    currentAdvancedCustom,
    currentBaseUrl,
    currentHeaderOverride,
    currentKey,
    currentModelMapping,
    currentModels,
    currentProxy,
    currentSettings,
    currentType,
    form,
    formValues,
    initialModels,
    isEditing,
    open,
    pendingDiscoveryOpen,
    queryClient,
    setConfigurationSection,
    setIsCodexCredentialRefreshing,
    setModelDiscoveryDialogOpen,
    setPendingDiscoveryOpen,
    setPendingErrorFocus,
    t,
  } = state
  const advancedCustomStats = useMemo(
    () => getAdvancedCustomStats(currentAdvancedCustom),
    [currentAdvancedCustom]
  )
  const advancedCustomRouteTypeLabels =
    advancedCustomStats.routeTypeLabels.slice(
      0,
      ADVANCED_CUSTOM_ROUTE_TYPE_PREVIEW_LIMIT
    )
  const hiddenAdvancedCustomRouteTypeCount =
    advancedCustomStats.routeTypeLabels.length -
    advancedCustomRouteTypeLabels.length
  const advancedCustomRouteTypeTitle =
    hiddenAdvancedCustomRouteTypeCount > 0
      ? advancedCustomStats.routeTypeLabels.join(', ')
      : undefined

  // Parse current models as array
  const currentModelsArray = useMemo(
    () => parseModelsString(currentModels),
    [currentModels]
  )

  // 当前上游协议（ui-spec §6.4）：归一化逻辑唯一实现于 getChannelProtocol，
  // 与渠道列表的「协议」列/工具栏筛选共用同一套判定。
  const currentProtocol: ChannelProtocol | '' = useMemo(
    () =>
      getChannelProtocol(
        currentType,
        parseChannelOtherSettings(formValues.settings)
      ) ?? '',
    [formValues.settings, currentType]
  )

  const selectChannelProtocol = useCallback(
    (protocol: ChannelProtocol) => {
      if (!canEditSensitive) return
      const option = CHANNEL_PROTOCOL_OPTIONS.find(
        (item) => item.value === protocol
      )
      if (!option) return
      form.setValue('type', option.type, { shouldDirty: true })
      form.setValue('protocol', protocol, { shouldDirty: true })
      if (!isEditing && !form.getValues('name').trim()) {
        form.setValue('name', t(CHANNEL_PROTOCOL_PRESENTATION[protocol].label))
      }
    },
    [canEditSensitive, isEditing, form, t]
  )

  // 下拉只给 4 个协议；编辑一个非协议型（旧厂商类型）渠道时，把当前类型作为
  // "当前值"选项保留，避免保存时被静默改写（ui-spec §6.4）。
  const channelProtocolComboboxOptions = useMemo(() => {
    const options = CHANNEL_PROTOCOL_OPTIONS.map((option) => ({
      value: `protocol:${option.value}`,
      label: t(CHANNEL_PROTOCOL_PRESENTATION[option.value].label),
      icon: <ChannelTypeLogo type={option.type} size={16} />,
    }))
    if (currentProtocol === '' && currentType > 0) {
      options.unshift({
        value: `type:${currentType}`,
        label: `#${currentType} (${t('Current')})`,
        icon: <ChannelTypeLogo type={currentType} size={16} />,
      })
    }
    return options
  }, [t, currentProtocol, currentType])

  const formErrors = form.formState.errors
  const providerPresetOptions = useMemo(
    () =>
      PROVIDER_PRESETS.map((preset) => ({
        value: preset.id,
        label: preset.name,
        icon: getLobeIcon(preset.icon, 16),
        description: preset.baseUrl.replace(/^https?:\/\//, ''),
      })),
    []
  )
  const configuration = getChannelConfigurationState(
    formValues,
    formErrors,
    isEditing
  )

  // Extract redirect models from model_mapping (target values)
  const redirectModelList = useMemo(
    () => extractRedirectModels(currentModelMapping || ''),
    [currentModelMapping]
  )

  // Extract source keys from model_mapping (models being remapped FROM)
  const redirectModelKeyList = useMemo(
    () => extractMappingSourceModels(currentModelMapping || ''),
    [currentModelMapping]
  )

  // Upstream names offered as mapping targets: aliases already mapped plus the
  // channel's declared models. The base "all declared models" union was a
  // leftover that surfaced unrelated channels' models (ui-spec §6.4).
  const mappingTargetOptions = useMemo(
    () => [...new Set([...redirectModelList, ...currentModelsArray])],
    [redirectModelList, currentModelsArray]
  )

  const modelMappingGuardrail = useMemo<ModelMappingGuardrail>(() => {
    if (!currentModelMapping?.trim()) {
      return createEmptyModelMappingGuardrail()
    }

    try {
      const parsed = JSON.parse(currentModelMapping)
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        return { ...createEmptyModelMappingGuardrail(), invalidJson: true }
      }

      const entries = Object.entries(parsed).reduce<
        Array<{ source: string; target: string }>
      >((acc, [rawSource, rawTarget]) => {
        const source = String(rawSource).trim()
        const target = String(rawTarget ?? '').trim()

        if (!source || !target) {
          return acc
        }

        acc.push({ source, target })
        return acc
      }, [])

      const missingSourceModels = [
        ...new Set(
          entries
            .filter(
              (entry) =>
                Boolean(entry.source) &&
                !currentModelsArray.includes(entry.source)
            )
            .map((entry) => entry.source)
        ),
      ]

      const exposedTargetModels = [
        ...new Set(
          entries
            .filter(
              (entry) =>
                Boolean(entry.target) &&
                currentModelsArray.includes(entry.target)
            )
            .map((entry) => entry.target)
        ),
      ]

      return {
        invalidJson: false,
        entries,
        missingSourceModels,
        exposedTargetModels,
      }
    } catch {
      return { ...createEmptyModelMappingGuardrail(), invalidJson: true }
    }
  }, [currentModelMapping, currentModelsArray])

  const mappingPreviewPairs =
    modelMappingGuardrail.entries.length > 0
      ? modelMappingGuardrail.entries.slice(0, 3)
      : MODEL_MAPPING_PREVIEW_FALLBACK
  const remainingMappingCount =
    modelMappingGuardrail.entries.length > 3
      ? modelMappingGuardrail.entries.length - 3
      : 0

  const upstreamUpdateMeta = useMemo(() => {
    const settings = parseSettingsRecord(currentSettings)
    const detectedModels = Array.isArray(
      settings.upstream_model_update_last_detected_models
    )
      ? settings.upstream_model_update_last_detected_models
          .map((model) => String(model || '').trim())
          .filter(Boolean)
      : []

    return {
      lastCheckTime: settings.upstream_model_update_last_check_time,
      detectedModels: [...new Set(detectedModels)],
    }
  }, [currentSettings])

  const upstreamDetectedModelsPreview = upstreamUpdateMeta.detectedModels.slice(
    0,
    UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT
  )
  const upstreamDetectedModelsOmittedCount =
    upstreamUpdateMeta.detectedModels.length -
    upstreamDetectedModelsPreview.length

  // Handle type change - set default values for specific types
  useEffect(() => {
    if (isEditing) return // Don't auto-set defaults when editing

    // Type 45 (VolcEngine) - set default base_url
    if (currentType === 45) {
      const currentBaseUrlValue = form.getValues('base_url')
      if (!currentBaseUrlValue || currentBaseUrlValue === '') {
        form.setValue('base_url', 'https://ark.cn-beijing.volces.com')
      }
    }

    // Type 18 (Xunfei) - set default other (version)
    if (currentType === 18) {
      const currentOther = form.getValues('other')
      if (!currentOther || currentOther === '') {
        form.setValue('other', 'v2.1')
      }
    }
  }, [currentType, isEditing, form])

  useEffect(() => {
    if (currentType !== 45 || currentBaseUrl !== 'doubao-coding-plan') return

    form.setValue('base_url', 'https://ark.cn-beijing.volces.com', {
      shouldDirty: false,
      shouldValidate: true,
    })
  }, [currentBaseUrl, currentType, form])

  const handleRefreshCodexCredential = useCallback(async () => {
    if (!channelId) return
    setIsCodexCredentialRefreshing(true)
    try {
      await refreshCodexCredential(channelId)
      toast.success(t('Credential refreshed'))
      queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.detail(channelId),
      })
    } catch (error) {
      handleServerError(error, t('Refresh failed'))
    } finally {
      setIsCodexCredentialRefreshing(false)
    }
  }, [channelId, queryClient, t])

  // Unified function to update models
  const updateModels = useCallback(
    (newModels: string[], merge: boolean = false) => {
      const finalModels = merge
        ? formatModelsArray([...currentModelsArray, ...newModels])
        : formatModelsArray(newModels)
      form.setValue('models', finalModels)
      return newModels.length
    },
    [currentModelsArray, form]
  )

  // Discovery source (ui-spec §6.4):
  //  - unsaved channel → preview with the draft connection (form key).
  //  - saved channel + operator with sensitive write + a freshly typed key →
  //    preview with the draft key, so a copied channel can be probed with its
  //    new key without saving first.
  //  - saved channel otherwise → reuse the stored credential (saved request):
  //    the form never repopulates the key, so a preview without a key would
  //    always 401.
  // Advanced Custom keeps its own preview path, which reuses the saved key on
  // the server via channel_id (the probe never sends the form key).
  const draftKeyProbing =
    isEditing &&
    canEditSensitive &&
    currentType !== CHANNEL_TYPE_ADVANCED_CUSTOM &&
    Boolean(currentKey?.trim())
  const previewModels =
    !isEditing ||
    (currentType === CHANNEL_TYPE_ADVANCED_CUSTOM && canEditSensitive) ||
    draftKeyProbing
  const canDiscoverModels = previewModels ? canEditSensitive : canOperateChannel
  const previewKey =
    isEditing && currentType === CHANNEL_TYPE_ADVANCED_CUSTOM
      ? undefined
      : currentKey
  const previewRequest = useMemo<ChannelModelDiscoveryRequest>(
    () => ({
      kind: 'preview',
      data: {
        type: currentType,
        key: previewKey,
        channel_id: isEditing ? channelId || undefined : undefined,
        base_url: currentBaseUrl || '',
        advanced_custom: currentAdvancedCustom,
        header_override: currentHeaderOverride,
        proxy: currentProxy,
      },
    }),
    [
      currentType,
      previewKey,
      isEditing,
      channelId,
      currentBaseUrl,
      currentAdvancedCustom,
      currentHeaderOverride,
      currentProxy,
    ]
  )
  const savedRequest = useMemo<ChannelModelDiscoveryRequest>(
    () => ({ kind: 'saved', channelId: channelId || 0 }),
    [channelId]
  )
  // Manual probing is allowed only once the connection fields the probe needs
  // are valid: type + key for unsaved channels, and the saved record for
  // existing ones (the saved request reuses the stored credential).
  const discoveryConnectionReady = useMemo(() => {
    if (!MODEL_FETCHABLE_TYPES.has(currentType)) return false
    if (!previewModels) return Boolean(channelData)
    if (
      !isEditing &&
      currentType !== CHANNEL_TYPE_ADVANCED_CUSTOM &&
      !currentKey?.trim()
    ) {
      return false
    }
    if (
      DISCOVERY_BASE_URL_REQUIRED_TYPES.has(currentType) &&
      !currentBaseUrl?.trim()
    ) {
      return false
    }
    return true
  }, [
    channelData,
    currentBaseUrl,
    currentKey,
    currentType,
    isEditing,
    previewModels,
  ])

  const discovery = useChannelModelDiscovery({
    enabled:
      open &&
      canDiscoverModels &&
      MODEL_FETCHABLE_TYPES.has(currentType) &&
      (!isEditing || Boolean(channelData)),
    request: previewModels ? previewRequest : savedRequest,
    // 探测一律手动触发（「探测上游模型」按钮），不自动拉取。
    autoFetch: false,
  })
  const fetchDiscoveredModels = discovery.fetch
  const handleFetchModels = useCallback(async () => {
    const type = form.getValues('type')
    if (!MODEL_FETCHABLE_TYPES.has(type)) {
      toast.error(t('This channel type does not support fetching models'))
      return
    }
    if (!canDiscoverModels) {
      toast.error(t("You don't have necessary permission"))
      return
    }
    if (
      !isEditing &&
      type !== CHANNEL_TYPE_ADVANCED_CUSTOM &&
      !form.getValues('key')?.trim()
    ) {
      form.setError('key', {
        type: 'manual',
        message: ERROR_MESSAGES.REQUIRED_KEY,
      })
      setConfigurationSection('connection')
      setPendingErrorFocus('key')
      return
    }
    setPendingDiscoveryOpen(true)
    await fetchDiscoveredModels()
  }, [isEditing, canDiscoverModels, form, t, fetchDiscoveredModels])

  // De-duplicate and trim upstream results before summarizing or merging them.
  const normalizedDiscoveredModels = useMemo(
    () => [
      ...new Set(discovery.models.map(normalizeModelName).filter(Boolean)),
    ],
    [discovery.models]
  )
  const discoveredNewModels = useMemo(() => {
    const existingModels = isEditing ? initialModels : currentModelsArray
    const existing = new Set(existingModels.map(normalizeModelName))
    return normalizedDiscoveredModels.filter((model) => !existing.has(model))
  }, [currentModelsArray, initialModels, isEditing, normalizedDiscoveredModels])

  // A successful probe opens the centered selection dialog (ui-spec §6.4).
  // Opening here instead of inside handleFetchModels keeps the hook untouched
  // and avoids a stale render window while the request is in flight.
  useEffect(() => {
    if (!pendingDiscoveryOpen) return
    if (discovery.status === 'success') {
      // An empty upstream result keeps the inline "no models returned" notice
      // instead of opening an empty picker.
      if (normalizedDiscoveredModels.length > 0) {
        setModelDiscoveryDialogOpen(true)
      }
      setPendingDiscoveryOpen(false)
    } else if (discovery.status === 'error') {
      setPendingDiscoveryOpen(false)
    }
  }, [
    discovery.status,
    pendingDiscoveryOpen,
    normalizedDiscoveredModels.length,
  ])

  let discoveryMessage = t(
    'Click "Probe upstream models" to fetch the model list from the upstream.'
  )
  if (discovery.status === 'loading') {
    discoveryMessage = t('Fetching models...')
  } else if (discovery.status === 'stale') {
    discoveryMessage = t(
      'Connection settings changed. Fetch models again to refresh the list.'
    )
  } else if (discovery.status === 'success') {
    discoveryMessage = t(
      'Found {{count}} upstream models · {{added}} new · {{existing}} existing',
      {
        count: normalizedDiscoveredModels.length,
        added: discoveredNewModels.length,
        existing:
          normalizedDiscoveredModels.length - discoveredNewModels.length,
      }
    )
  }

  return {
    advancedCustomStats,
    advancedCustomRouteTypeLabels,
    hiddenAdvancedCustomRouteTypeCount,
    advancedCustomRouteTypeTitle,
    currentModelsArray,
    currentProtocol,
    selectChannelProtocol,
    channelProtocolComboboxOptions,
    formErrors,
    providerPresetOptions,
    configuration,
    redirectModelList,
    redirectModelKeyList,
    mappingTargetOptions,
    modelMappingGuardrail,
    mappingPreviewPairs,
    remainingMappingCount,
    upstreamUpdateMeta,
    upstreamDetectedModelsPreview,
    upstreamDetectedModelsOmittedCount,
    handleRefreshCodexCredential,
    updateModels,
    draftKeyProbing,
    previewModels,
    canDiscoverModels,
    previewKey,
    previewRequest,
    savedRequest,
    discoveryConnectionReady,
    discovery,
    fetchDiscoveredModels,
    handleFetchModels,
    normalizedDiscoveredModels,
    discoveredNewModels,
    discoveryMessage,
  }
}

export type ChannelMutateDerivedState = ReturnType<
  typeof useChannelMutateDerivedState
>
