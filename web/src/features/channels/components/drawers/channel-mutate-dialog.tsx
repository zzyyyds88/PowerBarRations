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
import {
  ArrowRight,
  ClipboardPaste,
  ListPlus,
  Loader2,
  Server,
  Trash2,
  Copy,
  FileText,
  Eraser,
  Plus,
  RefreshCw,
  Code,
  Route,
  Settings,
  SlidersHorizontal,
  Wand2,
  X,
} from 'lucide-react'
import {
  type ReactNode,
  useEffect,
  useState,
  useMemo,
  useCallback,
  useRef,
} from 'react'
import { type SubmitErrorHandler, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DIALOG_SIZE_CLASS } from '@/components/dialog-size'
import { sideDrawerSwitchItemClassName } from '@/components/drawer-layout'
import { ErrorState } from '@/components/error-state'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { JsonEditor } from '@/components/json-editor'
import { LearnMore } from '@/components/learn-more'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import {
  Dialog as DialogRoot,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { Input } from '@/components/ui/input'
import { PopoverDescription, PopoverTitle } from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
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
import { getServerErrorMessage } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  fetchUpstreamModelsBatch,
  getChannel,
  getChannelDefaultBaseURLs,
  refreshCodexCredential,
} from '../../api'
import {
  ADD_MODE_OPTIONS,
  CLAUDE_FIELD_PASSTHROUGH_TYPES,
  CHANNEL_PROTOCOL_OPTIONS,
  CHANNEL_PROTOCOL_PRESENTATION,
  CHANNEL_STATUS_LABELS,
  CHANNEL_TYPE_NEW_API,
  CHANNEL_TYPE_OPTIONS,
  CHANNEL_TYPE_WARNINGS,
  ERROR_MESSAGES,
  FIELD_PASSTHROUGH_TYPES,
  FIELD_DESCRIPTIONS,
  FIELD_PLACEHOLDERS,
  MODEL_FETCHABLE_TYPES,
  OPENAI_FIELD_PASSTHROUGH_TYPES,
} from '../../constants'
import {
  useChannelModelDiscovery,
  type ChannelModelDiscoveryRequest,
} from '../../hooks/use-channel-model-discovery'
import { useChannelMutateForm } from '../../hooks/use-channel-mutate-form'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  CHANNEL_TYPE_ADVANCED_CUSTOM,
  channelFormSchema,
  channelsQueryKeys,
  getAdvancedCustomStats,
  getChannelProtocol,
  parseChannelOtherSettings,
  transformChannelToFormDefaults,
  type ChannelFormValues,
  deduplicateKeys,
  getKeyPromptForType,
  parseModelsString,
  formatModelsArray,
  extractRedirectModels,
  extractMappingSourceModels,
  hasModelConfigChanged,
  findMissingModelsInMapping,
  normalizeModelName,
  validateModelMappingJson,
} from '../../lib'
import {
  getChannelConfigurationSection,
  getChannelConfigurationState,
  type ChannelConfigurationStatus,
  type ChannelConfigurationSection,
} from '../../lib/channel-configuration'
import {
  collectInvalidStatusCodeEntries,
  collectNewDisallowedStatusCodeRedirects,
} from '../../lib/status-code-risk-guard'
import {
  isChannelProtocol,
  type Channel,
  type ChannelProtocol,
} from '../../types'
import { ChannelPricesEditor } from '../channel-prices-editor'
import { ChannelTypeLogo } from '../channel-type-badge'
import { useChannels } from '../channels-provider'
import { AdvancedCustomEditorDialog } from '../dialogs/advanced-custom-editor-dialog'
import { ConfigureModelsDialog } from '../dialogs/configure-models-dialog'
import { FetchModelsDialog } from '../dialogs/fetch-models-dialog'
import {
  MissingModelsConfirmationDialog,
  type MissingModelsAction,
} from '../dialogs/missing-models-confirmation-dialog'
import { ParamOverrideEditorDialog } from '../dialogs/param-override-editor-dialog'
import { StatusCodeRiskDialog } from '../dialogs/status-code-risk-dialog'
import { ModelMappingEditor } from '../model-mapping-editor'
import {
  ChannelConfiguration,
  ChannelConfigurationStatusIndicator,
} from './channel-configuration'
import {
  ChannelApiAccessSection,
  ChannelAuthSection,
  ChannelBasicSection,
  ChannelEditorLoadingState,
  ChannelModelsSection,
} from './sections'

type ChannelMutateDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: Channel | null
}

type ModelMappingGuardrail = {
  invalidJson: boolean
  entries: Array<{ source: string; target: string }>
  missingSourceModels: string[]
  exposedTargetModels: string[]
}

// Helper functions
const createEmptyModelMappingGuardrail = (): ModelMappingGuardrail => ({
  invalidJson: false,
  entries: [],
  missingSourceModels: [],
  exposedTargetModels: [],
})

const formatModelNames = (models: string[]): string =>
  models.map((model) => `"${model}"`).join(', ')

const MODEL_MAPPING_PREVIEW_FALLBACK: Array<{
  source: string
  target: string
}> = [{ source: 'client-model', target: 'upstream-model' }]

const ADVANCED_CUSTOM_ROUTE_TYPE_PREVIEW_LIMIT = 3
const UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT = 8
// Channel types whose upstream requires an explicit base URL before probing.
const DISCOVERY_BASE_URL_REQUIRED_TYPES = new Set([
  3,
  8,
  36,
  45,
  CHANNEL_TYPE_NEW_API,
])
const SENSITIVE_FORM_FIELDS = [
  'type',
  'base_url',
  'key',
  'other',
  'key_mode',
  'param_override',
  'header_override',
  'settings',
  'setting',
  'advanced_custom',
  'is_enterprise_account',
  'vertex_key_type',
  'aws_key_type',
  'azure_responses_version',
  'force_format',
  'thinking_to_content',
  'proxy',
  'http_protocol',
  'http2_connection_shards',
  'pass_through_body_enabled',
  'system_prompt',
  'system_prompt_override',
  'allow_service_tier',
  'disable_store',
  'allow_safety_identifier',
  'allow_include_obfuscation',
  'allow_inference_geo',
  'allow_speed',
  'claude_beta_query',
  'upstream_model_update_check_enabled',
  'upstream_model_update_auto_sync_enabled',
  'upstream_model_update_ignored_models',
] satisfies (keyof ChannelFormValues)[]

function parseSettingsRecord(
  settings: string | undefined
): Record<string, unknown> {
  if (!settings?.trim()) return {}
  try {
    const parsed = JSON.parse(settings)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>
    }
  } catch {
    return {}
  }
  return {}
}

function formatUnixTime(timestamp: unknown): string {
  const seconds = Number(timestamp)
  if (!Number.isFinite(seconds) || seconds <= 0) return '-'
  return new Date(seconds * 1000).toLocaleString()
}

function channelConfigurationBlockClassName(
  status: ChannelConfigurationStatus,
  className?: string
) {
  return cn(
    'border-border/60 rounded-lg border p-4 transition-colors',
    status === 'configured' && 'border-primary/35 ring-primary/20 ring-1',
    status === 'error' && 'border-destructive/50 ring-destructive/20 ring-1',
    className
  )
}

function CardHeading(props: {
  title: string
  icon?: ReactNode
  iconTone?: IconBadgeTone
  status?: ChannelConfigurationStatus
}) {
  return (
    <div className='flex items-center gap-3'>
      {props.icon && (
        <IconBadge tone={props.iconTone} size='md'>
          {props.icon}
        </IconBadge>
      )}
      <h3 className='min-w-0 flex-1 text-sm font-semibold tracking-tight'>
        {props.title}
      </h3>
      {props.status && (
        <ChannelConfigurationStatusIndicator status={props.status} />
      )}
    </div>
  )
}

function SubHeading(props: {
  title: string
  icon?: ReactNode
  iconTone?: IconBadgeTone
  status?: ChannelConfigurationStatus
}) {
  return (
    <div className='flex items-center gap-2'>
      {props.icon && (
        <IconBadge tone={props.iconTone} size='xs'>
          {props.icon}
        </IconBadge>
      )}
      <h4 className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
        {props.title}
      </h4>
      {props.status && (
        <ChannelConfigurationStatusIndicator status={props.status} />
      )}
    </div>
  )
}

export function ChannelMutateDialog({
  open,
  onOpenChange,
  currentRow,
}: ChannelMutateDialogProps) {
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
  const channelFormRef = useRef<HTMLFormElement>(null)
  const [modelDiscoveryDialogOpen, setModelDiscoveryDialogOpen] =
    useState(false)
  const [fetchModelsDialogOpen, setFetchModelsDialogOpen] = useState(false)
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

  // Form setup
  const form = useForm<ChannelFormValues>({
    resolver: zodResolver(channelFormSchema),
    defaultValues: CHANNEL_FORM_DEFAULT_VALUES,
  })

  // Watch values once for conditional fields and configuration indicators.
  const formValues = form.watch()
  const multiKeyMode = formValues.multi_key_mode
  const multiKeyType = formValues.multi_key_type
  const keyMode = formValues.key_mode
  const currentType = formValues.type
  const baseUrlPlaceholder =
    defaultBaseURLs?.[currentType] || t(FIELD_PLACEHOLDERS.BASE_URL)
  const currentStatus = formValues.status
  const currentBaseUrl = formValues.base_url
  const currentKey = formValues.key
  const currentModels = formValues.models
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
  const supportsMultiKeyAddMode =
    currentType !== 57 && !(currentType === 41 && vertexKeyType === 'api_key')
  const addModeOptions = useMemo(
    () =>
      supportsMultiKeyAddMode
        ? ADD_MODE_OPTIONS
        : ADD_MODE_OPTIONS.filter((option) => option.value === 'single'),
    [supportsMultiKeyAddMode]
  )

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
      const legacyLabel =
        CHANNEL_TYPE_OPTIONS.find((option) => option.value === currentType)
          ?.label ?? `#${currentType}`
      options.unshift({
        value: `type:${currentType}`,
        label: `${t(legacyLabel)} (${t('Current')})`,
        icon: <ChannelTypeLogo type={currentType} size={16} />,
      })
    }
    return options
  }, [t, currentProtocol, currentType])

  const formErrors = form.formState.errors
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

  useEffect(() => {
    if (isEditing || supportsMultiKeyAddMode) return
    if (multiKeyMode && multiKeyMode !== 'single') {
      form.setValue('multi_key_mode', 'single', {
        shouldDirty: true,
        shouldValidate: true,
      })
    }
  }, [form, isEditing, multiKeyMode, supportsMultiKeyAddMode])

  // Handle key deduplication
  const handleDeduplicateKeys = () => {
    const currentKey = form.getValues('key')
    if (!currentKey || currentKey.trim() === '') {
      toast.info(t('Please enter keys first'))
      return
    }

    const result = deduplicateKeys(currentKey)

    if (result.removedCount === 0) {
      toast.info(t('No duplicate keys found'))
    } else {
      form.setValue('key', result.deduplicatedText)
      toast.success(
        t(
          'Removed {{removed}} duplicate key(s). Before: {{before}}, After: {{after}}',
          {
            removed: result.removedCount,
            before: result.beforeCount,
            after: result.afterCount,
          }
        )
      )
    }
  }

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

  // Ordinary edits use the saved channel. Advanced Custom retains its existing
  // preview path, which reuses the saved credential on the server.
  const previewModels =
    !isEditing ||
    (currentType === CHANNEL_TYPE_ADVANCED_CUSTOM && canEditSensitive)
  const canDiscoverModels = previewModels ? canEditSensitive : canOperateChannel
  const previewKey = isEditing ? undefined : currentKey
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
    const existingModels = isEditing
      ? initialModelsRef.current
      : currentModelsArray
    const existing = new Set(existingModels.map(normalizeModelName))
    return normalizedDiscoveredModels.filter((model) => !existing.has(model))
  }, [currentModelsArray, isEditing, normalizedDiscoveredModels])

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

  // 「获取模型列表」入口（New API 形态）：优先用当前表单里的草稿连接信息探测，
  // 草稿不完整时回退到已保存渠道（按名寻址）。两者都走稳定面
  // POST /api/channels/batch/fetch-models，成功即裸 {models:[...]}。
  const fetchModelsDialogFetcher = useMemo(() => {
    if (!canDiscoverModels) return undefined
    const channelName = channelData?.name
    return async (): Promise<string[]> => {
      const type = form.getValues('type')
      if (!MODEL_FETCHABLE_TYPES.has(type)) {
        throw new Error(t('This channel type does not support fetching models'))
      }
      const draftReady =
        !isEditing ||
        type === CHANNEL_TYPE_ADVANCED_CUSTOM ||
        Boolean(form.getValues('key')?.trim()) ||
        Boolean(form.getValues('base_url')?.trim())
      if (draftReady) {
        return fetchUpstreamModelsBatch({
          type,
          base_url: form.getValues('base_url') || '',
          key: form.getValues('key')?.trim() || undefined,
          advanced_custom: form.getValues('advanced_custom') || undefined,
          header_override: form.getValues('header_override') || undefined,
          proxy: form.getValues('proxy') || undefined,
        })
      }
      if (!channelName) {
        throw new Error(t('No channel selected'))
      }
      return fetchUpstreamModelsBatch({ channel: channelName })
    }
  }, [canDiscoverModels, channelData?.name, form, isEditing, t])

  const handleFetchModelsToForm = useCallback(
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
      onOpenChange(v)
      if (!v) {
        form.reset(CHANNEL_FORM_DEFAULT_VALUES)
        setClipboardConnectionInfo(null)
      }
    },
    [onOpenChange, form, isSubmitting]
  )

  const proxyFields = (
    <FormField
      control={form.control}
      name='proxy'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Proxy Address')}</FormLabel>
          <FormControl>
            <Input placeholder={t('socks5://user:pass@host:port')} {...field} />
          </FormControl>
          <FormDescription>
            {t(
              'Network proxy for this channel (supports HTTP, HTTPS, SOCKS5, and SOCKS5H)'
            )}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  const pbrPricesFields = (
    <FormField
      control={form.control}
      name='pbr_prices'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Upstream unit prices')}</FormLabel>
          <FormControl>
            <ChannelPricesEditor
              value={field.value ?? []}
              models={currentModelsArray}
              onChange={field.onChange}
              disabled={sensitiveLocked}
            />
          </FormControl>
          <FormDescription>
            {t(
              'CNY per 1M tokens, used for cost accounting only. Models without a channel price are not converted.'
            )}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  const systemPromptFields = (
    <FormField
      control={form.control}
      name='system_prompt'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('System Prompt')}</FormLabel>
          <FormControl>
            <Textarea
              placeholder={t(
                'Enter system prompt (user prompt takes priority)'
              )}
              rows={3}
              {...field}
            />
          </FormControl>
          <FormDescription>
            {t('Default system prompt for this channel')}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  const systemPromptOverrideFields = (
    <FormField
      control={form.control}
      name='system_prompt_override'
      render={({ field }) => (
        <FormItem className='flex items-center justify-between'>
          <div className='space-y-0.5'>
            <FormLabel>{t('System Prompt Concatenation')}</FormLabel>
            <FormDescription>
              {t('Concatenate channel system prompt with user&apos;s prompt')}
            </FormDescription>
          </div>
          <FormControl>
            <Switch
              disabled={sensitiveLocked}
              checked={field.value}
              onCheckedChange={field.onChange}
            />
          </FormControl>
        </FormItem>
      )}
    />
  )

  const passthroughFields = (
    <FormField
      control={form.control}
      name='pass_through_body_enabled'
      render={({ field }) => (
        <FormItem className='flex items-center justify-between px-4 py-3'>
          <div className='space-y-0.5'>
            <FormLabel>{t('Pass Through Body')}</FormLabel>
            <FormDescription>
              {t('Pass request body directly to upstream')}
            </FormDescription>
          </div>
          <FormControl>
            <Switch
              disabled={sensitiveLocked}
              checked={field.value}
              onCheckedChange={field.onChange}
            />
          </FormControl>
        </FormItem>
      )}
    />
  )

  const thinkingFields = (
    <FormField
      control={form.control}
      name='thinking_to_content'
      render={({ field }) => (
        <FormItem className='flex items-center justify-between px-4 py-3'>
          <div className='space-y-0.5'>
            <FormLabel>{t('Thinking to Content')}</FormLabel>
            <FormDescription>
              {t('Convert reasoning_content to <think> tag in content')}
            </FormDescription>
          </div>
          <FormControl>
            <Switch
              disabled={sensitiveLocked}
              checked={field.value}
              onCheckedChange={field.onChange}
            />
          </FormControl>
        </FormItem>
      )}
    />
  )

  const formatFields = currentType === 1 && (
    <FormField
      control={form.control}
      name='force_format'
      render={({ field }) => (
        <FormItem className='flex items-center justify-between px-4 py-3'>
          <div className='space-y-0.5'>
            <FormLabel>{t('Force Format')}</FormLabel>
            <FormDescription>
              {t(
                'Force format response to OpenAI standard (OpenAI channel only)'
              )}
            </FormDescription>
          </div>
          <FormControl>
            <Switch
              disabled={sensitiveLocked}
              checked={field.value}
              onCheckedChange={field.onChange}
            />
          </FormControl>
        </FormItem>
      )}
    />
  )

  const notesFields = (
    <div
      role='group'
      aria-label={t('Internal Notes')}
      className={channelConfigurationBlockClassName(
        configuration.blocks.internalNotes,
        'flex flex-col gap-4'
      )}
    >
      <SubHeading
        title={t('Internal Notes')}
        status={configuration.blocks.internalNotes}
        icon={<FileText className='h-3.5 w-3.5' />}
        iconTone='chart-3'
      />
      <div className='grid gap-4 sm:grid-cols-2'>
        <FormField
          control={form.control}
          name='tag'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Tag')}</FormLabel>
              <FormControl>
                <Input placeholder={t(FIELD_PLACEHOLDERS.TAG)} {...field} />
              </FormControl>
              <FormDescription>{t(FIELD_DESCRIPTIONS.TAG)}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='remark'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Remark')}</FormLabel>
              <FormControl>
                <Textarea
                  placeholder={t(FIELD_PLACEHOLDERS.REMARK)}
                  rows={2}
                  {...field}
                />
              </FormControl>
              <FormDescription>{t(FIELD_DESCRIPTIONS.REMARK)}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>
    </div>
  )

  const httpProtocolFields = (
    <FormField
      control={form.control}
      name='http_protocol'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('HTTP Protocol')}</FormLabel>
          <Select
            disabled={sensitiveLocked}
            items={[
              {
                value: 'auto',
                label: t('Auto'),
              },
              {
                value: 'http1',
                label: t('HTTP/1.1'),
              },
            ]}
            value={field.value || 'auto'}
            onValueChange={(value) => {
              const nextProtocol = value === 'http1' ? 'http1' : 'auto'
              field.onChange(nextProtocol)
              if (nextProtocol === 'http1') {
                form.setValue('http2_connection_shards', 1, {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
            }}
          >
            <FormControl>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
            </FormControl>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='auto'>{t('Auto')}</SelectItem>
                <SelectItem value='http1'>{t('HTTP/1.1')}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <FormDescription>
            {t(
              'Auto negotiates HTTP/2 when available. HTTP/1.1 forces multiple keep-alive connections under concurrency.'
            )}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  const httpShardsFields = (
    <FormField
      control={form.control}
      name='http2_connection_shards'
      render={({ field }) => {
        const http1Selected = currentHttpProtocol === 'http1'
        const shardItems = Array.from({ length: 8 }, (_, index) => {
          const value = String(index + 1)
          return { value, label: value }
        })
        return (
          <FormItem>
            <FormLabel>{t('HTTP/2 Connection Shards')}</FormLabel>
            <Select
              items={shardItems}
              value={String(field.value || 1)}
              disabled={sensitiveLocked || http1Selected}
              onValueChange={(value) => {
                field.onChange(Number(value))
              }}
            >
              <FormControl>
                <SelectTrigger disabled={sensitiveLocked || http1Selected}>
                  <SelectValue />
                </SelectTrigger>
              </FormControl>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {shardItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <FormDescription>
              {http1Selected
                ? t(
                    'HTTP/2 connection shards are unavailable when HTTP/1.1 is selected.'
                  )
                : t(
                    'Spread HTTP/2 traffic across multiple reusable connections to the same upstream origin (1-8).'
                  )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )
      }}
    />
  )

  const routingFields = (
    <div
      role='group'
      aria-label={t('Routing Strategy')}
      className={channelConfigurationBlockClassName(
        configuration.blocks.routingStrategy,
        'flex flex-col gap-4'
      )}
    >
      <SubHeading
        title={t('Routing Strategy')}
        status={configuration.blocks.routingStrategy}
        icon={<Route className='h-3.5 w-3.5' />}
        iconTone='info'
      />
      <FormField
        control={form.control}
        name='test_model'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Test Model')}</FormLabel>
            <FormControl>
              <Input
                placeholder={t(FIELD_PLACEHOLDERS.TEST_MODEL)}
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t(FIELD_DESCRIPTIONS.TEST_MODEL)}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  )

  const upstreamModelDetectionFields = MODEL_FETCHABLE_TYPES.has(
    currentType
  ) && (
    <div
      role='group'
      aria-label={t('Upstream Model Detection Settings')}
      className={channelConfigurationBlockClassName(
        configuration.blocks.upstreamModelDetection,
        'flex flex-col gap-4'
      )}
    >
      <CardHeading
        title={t('Upstream Model Detection Settings')}
        status={configuration.blocks.upstreamModelDetection}
        icon={<RefreshCw className='h-4 w-4' />}
        iconTone='info'
      />
      <fieldset
        disabled={sensitiveLocked}
        className='space-y-4 disabled:opacity-60'
      >
        <div className='divide-border space-y-0 divide-y border-y'>
          <FormField
            control={form.control}
            name='upstream_model_update_check_enabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between px-4 py-3'>
                <div className='space-y-0.5'>
                  <FormLabel>{t('Upstream Model Update Check')}</FormLabel>
                  <FormDescription>
                    {t('Periodically check for upstream model changes')}
                  </FormDescription>
                  <FormMessage />
                </div>
                <FormControl>
                  <Switch
                    disabled={sensitiveLocked}
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='upstream_model_update_auto_sync_enabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between px-4 py-3'>
                <div className='space-y-0.5'>
                  <FormLabel>{t('Auto Sync Upstream Models')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Automatically sync model list when upstream changes are detected'
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    disabled={
                      sensitiveLocked || !upstreamModelUpdateCheckEnabled
                    }
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />
        </div>
        <FormField
          control={form.control}
          name='upstream_model_update_ignored_models'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Ignored upstream models')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t(
                    'e.g., gpt-4.1-nano,regex:^claude-.*$,regex:^sora-.*$'
                  )}
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Comma-separated exact model names. Prefix with regex: to ignore by regular expression.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <div className='text-muted-foreground space-y-2 border-t pt-3 text-xs'>
          <div>
            <span className='text-foreground font-medium'>
              {t('Last check time')}:
            </span>{' '}
            {formatUnixTime(upstreamUpdateMeta.lastCheckTime)}
          </div>
          <div>
            <span className='text-foreground font-medium'>
              {t('Last detected addable models')}:
            </span>{' '}
            {upstreamUpdateMeta.detectedModels.length === 0 ? (
              t('None')
            ) : (
              <>
                <span className='break-all'>
                  {upstreamDetectedModelsPreview.join(', ')}
                </span>
                {upstreamDetectedModelsOmittedCount > 0 && (
                  <span className='ml-1'>
                    {t('({{total}} total, {{omit}} omitted)', {
                      total: upstreamUpdateMeta.detectedModels.length,
                      omit: upstreamDetectedModelsOmittedCount,
                    })}
                  </span>
                )}
              </>
            )}
          </div>
        </div>
      </fieldset>
    </div>
  )

  const modelMappingFields = (
    <div
      role='group'
      aria-label={t('Model Mapping')}
      className={channelConfigurationBlockClassName(
        configuration.blocks.modelMapping
      )}
    >
      <FormField
        control={form.control}
        name='model_mapping'
        render={({ field }) => (
          <FormItem className='space-y-3'>
            <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
              <div className='space-y-1'>
                <div className='flex items-center gap-2'>
                  <FormLabel className='mb-0'>{t('Model Mapping')}</FormLabel>
                  <ChannelConfigurationStatusIndicator
                    status={configuration.blocks.modelMapping}
                  />
                  <LearnMore
                    triggerProps={{
                      type: 'button',
                      'aria-label': t('How model mapping works'),
                      className: 'size-4 rounded-full',
                    }}
                    contentProps={{
                      collisionPadding: 16,
                      className:
                        'w-96 max-w-[calc(100vw-2rem)] gap-3 p-4 text-sm leading-6',
                    }}
                  >
                    <PopoverTitle className='text-foreground'>
                      {t('Request flow')}
                    </PopoverTitle>
                    <div className='space-y-2 font-mono'>
                      {mappingPreviewPairs.map((pair) => (
                        <div
                          key={`${pair.source}-${pair.target}`}
                          className='grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-start gap-3'
                        >
                          <span className='min-w-0 wrap-anywhere'>
                            {pair.source}
                          </span>
                          <ArrowRight
                            className='mt-1 size-4 shrink-0'
                            aria-hidden='true'
                          />
                          <span className='min-w-0 wrap-anywhere'>
                            {pair.target}
                          </span>
                        </div>
                      ))}
                      {remainingMappingCount > 0 && (
                        <div>
                          +{remainingMappingCount} {t('more mapping')}
                          {remainingMappingCount > 1 ? 's' : ''}
                        </div>
                      )}
                    </div>
                    <PopoverDescription className='leading-6'>
                      {t(
                        'Users call the model on the left. The platform forwards the request to the upstream model on the right.'
                      )}
                    </PopoverDescription>
                  </LearnMore>
                </div>
                <FormDescription>
                  {t(FIELD_DESCRIPTIONS.MODEL_MAPPING)}
                </FormDescription>
              </div>
            </div>
            <FormControl>
              <ModelMappingEditor
                value={field.value || ''}
                onChange={field.onChange}
                disabled={isSubmitting}
                sourceModelOptions={currentModelsArray}
                targetModelOptions={mappingTargetOptions}
              />
            </FormControl>
            {modelMappingGuardrail.invalidJson && (
              <Alert variant='destructive'>
                <AlertDescription>
                  {t('Model Mapping must be a JSON object like')}{' '}
                  <code className='font-mono'>{'{"gpt-4":"Azure-GPT4"}'}</code>
                  {t('. Please fix the JSON before saving.')}
                </AlertDescription>
              </Alert>
            )}
            {modelMappingGuardrail.missingSourceModels.length > 0 && (
              <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
                <AlertDescription className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                  <span>
                    {t('Add')}{' '}
                    {formatModelNames(
                      modelMappingGuardrail.missingSourceModels
                    )}{' '}
                    {t(
                      'to the Models list so users can use them before the mapping sends traffic upstream.'
                    )}
                  </span>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => {
                      updateModels([
                        ...currentModelsArray,
                        ...modelMappingGuardrail.missingSourceModels,
                      ])
                    }}
                  >
                    {t('Add missing models')}
                  </Button>
                </AlertDescription>
              </Alert>
            )}
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  )

  const basicSection = (
    <div className='scroll-mt-4'>
      <ChannelBasicSection>
        <div className='grid gap-4'>
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel required>{t('Name')}</FormLabel>
                <FormControl>
                  <Input placeholder={t(FIELD_PLACEHOLDERS.NAME)} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='type'
            render={({ field }) => {
              // 协议已确定 → 显示协议；旧厂商类型 → 显示"当前值"选项。
              const comboboxValue =
                currentProtocol !== ''
                  ? `protocol:${currentProtocol}`
                  : `type:${field.value ?? ''}`
              return (
                <FormItem>
                  <FormLabel required>{t('Protocol')}</FormLabel>
                  <FormControl>
                    <Combobox
                      options={channelProtocolComboboxOptions}
                      value={comboboxValue}
                      onValueChange={(value) => {
                        if (!value) return
                        if (value.startsWith('protocol:')) {
                          const parsed = value.slice('protocol:'.length)
                          if (isChannelProtocol(parsed)) {
                            selectChannelProtocol(parsed)
                          }
                        }
                        // `type:<n>` 是旧厂商类型的"当前值"选项：选中即保持原类型不变。
                      }}
                      disabled={sensitiveLocked || isSubmitting}
                      placeholder={t('Protocol')}
                      searchPlaceholder={t('Search...')}
                      aria-label={t('Protocol')}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )
            }}
          />
        </div>

        {!isEditing && (
          <FormField
            control={form.control}
            name='status'
            render={({ field }) => (
              <FormItem className={sideDrawerSwitchItemClassName()}>
                <div className='flex flex-col gap-0.5'>
                  <FormLabel>{t('Enabled')}</FormLabel>
                  <FormDescription className='text-xs'>
                    {t('Enable or disable this channel')}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value === 1}
                    onCheckedChange={(checked) =>
                      field.onChange(checked ? 1 : 2)
                    }
                  />
                </FormControl>
              </FormItem>
            )}
          />
        )}
      </ChannelBasicSection>
    </div>
  )

  const overrideFields = (
    <div
      role='group'
      aria-label={t('Override Rules')}
      className={channelConfigurationBlockClassName(
        configuration.blocks.overrideRules,
        'flex flex-col gap-4'
      )}
    >
      <SubHeading
        title={t('Override Rules')}
        status={configuration.blocks.overrideRules}
        icon={<Code className='h-3.5 w-3.5' />}
        iconTone='chart-4'
      />

      <FormField
        control={form.control}
        name='status_code_mapping'
        render={({ field }) => (
          <FormItem className='space-y-3'>
            <div className='space-y-1'>
              <FormLabel>{t('Status Code Mapping')}</FormLabel>
              <FormDescription>
                {t('Map upstream status codes to different codes')}
              </FormDescription>
            </div>
            <FormControl>
              <JsonEditor
                value={field.value || ''}
                onChange={field.onChange}
                disabled={isSubmitting}
                keyPlaceholder='400'
                valuePlaceholder='500'
                keyLabel='Original Code'
                valueLabel='Mapped Code'
                emptyMessage={t('No status code mappings configured.')}
                template={{ '400': '500', '429': '503' }}
                valueType='string'
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      {sensitiveLocked && (
        <p className='text-muted-foreground text-xs'>
          {t('No permission to perform this action')}
        </p>
      )}
      <fieldset
        disabled={sensitiveLocked}
        className='space-y-4 disabled:opacity-60'
      >
        <FormField
          control={form.control}
          name='param_override'
          render={({ field }) => (
            <FormItem className='space-y-3 border-t pt-4'>
              <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
                <div className='space-y-1'>
                  <FormLabel>{t('Parameter Override')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Override request parameters. Cannot override stream parameter.'
                    )}
                  </FormDescription>
                </div>
                <div className='flex flex-wrap gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => setParamOverrideEditorOpen(true)}
                  >
                    <Wand2 className='mr-2 h-4 w-4' />
                    {t('Visual edit')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => {
                      field.onChange(
                        JSON.stringify(
                          {
                            operations: [
                              {
                                path: 'temperature',
                                mode: 'set',
                                value: 0.7,
                                conditions: [
                                  {
                                    path: 'model',
                                    mode: 'prefix',
                                    value: 'gpt',
                                  },
                                ],
                                logic: 'AND',
                              },
                            ],
                          },
                          null,
                          2
                        )
                      )
                    }}
                  >
                    <Code className='mr-2 h-4 w-4' />
                    {t('New Format Template')}
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    onClick={() => field.onChange('')}
                  >
                    {t('Clear')}
                  </Button>
                </div>
              </div>
              <FormControl>
                <JsonCodeEditor
                  value={field.value || ''}
                  onChange={field.onChange}
                  name={field.name}
                  onBlur={field.onBlur}
                  textareaRef={field.ref}
                  disabled={sensitiveLocked || isSubmitting}
                  placeholder={t(
                    'Override request parameters. Cannot override stream parameter.'
                  )}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='header_override'
          render={({ field }) => (
            <FormItem className='space-y-3 border-t pt-4'>
              <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
                <div className='space-y-1'>
                  <FormLabel>{t('Request Header Override')}</FormLabel>
                  <FormDescription>
                    {t('Override request headers')}
                  </FormDescription>
                </div>
                <div className='flex flex-wrap gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      field.onChange(
                        JSON.stringify(
                          {
                            '*': true,
                            're:^X-Trace-.*$': true,
                            'X-Foo': '{client_header:X-Foo}',
                            Authorization: 'Bearer {api_key}',
                          },
                          null,
                          2
                        )
                      )
                    }
                  >
                    {t('Fill Template')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      field.onChange(JSON.stringify({ '*': true }, null, 2))
                    }
                  >
                    {t('Passthrough Template')}
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    onClick={() => field.onChange('')}
                  >
                    {t('Clear')}
                  </Button>
                </div>
              </div>
              <FormControl>
                <JsonCodeEditor
                  value={field.value || ''}
                  onChange={field.onChange}
                  name={field.name}
                  onBlur={field.onBlur}
                  textareaRef={field.ref}
                  disabled={sensitiveLocked || isSubmitting}
                  placeholder={t('Enter JSON to override request headers')}
                  heightClassName='h-40 min-h-40 max-h-40'
                />
              </FormControl>
              <FormDescription className='text-xs'>
                {t('Supported variables')}:{' '}
                <code className='bg-muted rounded px-1 py-0.5'>
                  {'{api_key}'}
                </code>{' '}
                — {t('Channel key')},{' '}
                <code className='bg-muted rounded px-1 py-0.5'>
                  {'{client_header:NAME}'}
                </code>{' '}
                — {t('Client header value')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </fieldset>
    </div>
  )

  const fieldPassthroughFields = FIELD_PASSTHROUGH_TYPES.has(currentType) && (
    <div
      role='group'
      aria-label={t('Field passthrough controls')}
      className={channelConfigurationBlockClassName(
        configuration.blocks.fieldPassthrough,
        'flex flex-col gap-4'
      )}
    >
      <CardHeading
        title={t('Field passthrough controls')}
        status={configuration.blocks.fieldPassthrough}
        icon={<SlidersHorizontal className='h-4 w-4' />}
        iconTone='chart-4'
      />
      <fieldset disabled={sensitiveLocked} className='disabled:opacity-60'>
        <div className='divide-border space-y-0 divide-y border-y'>
          <FormField
            control={form.control}
            name='allow_service_tier'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-sm'>
                    {t('Allow service_tier passthrough')}
                  </FormLabel>
                  <FormDescription>
                    {t('Pass through the service_tier field')}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    disabled={sensitiveLocked}
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          {OPENAI_FIELD_PASSTHROUGH_TYPES.has(currentType) && (
            <>
              <FormField
                control={form.control}
                name='disable_store'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                    <div className='space-y-0.5'>
                      <FormLabel className='text-sm'>
                        {t('Disable store passthrough')}
                      </FormLabel>
                      <FormDescription>
                        {t('When enabled, the store field will be blocked')}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        disabled={sensitiveLocked}
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='allow_safety_identifier'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                    <div className='space-y-0.5'>
                      <FormLabel className='text-sm'>
                        {t('Allow safety_identifier passthrough')}
                      </FormLabel>
                      <FormDescription>
                        {t('Pass through the safety_identifier field')}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        disabled={sensitiveLocked}
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='allow_include_obfuscation'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                    <div className='space-y-0.5'>
                      <FormLabel className='text-sm'>
                        {t('Allow include usage obfuscation passthrough')}
                      </FormLabel>
                      <FormDescription>
                        {t(
                          'Pass through the include field for usage obfuscation'
                        )}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        disabled={sensitiveLocked}
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='allow_inference_geo'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                    <div className='space-y-0.5'>
                      <FormLabel className='text-sm'>
                        {t('Allow inference geography passthrough')}
                      </FormLabel>
                      <FormDescription>
                        {t(
                          'Pass through the inference_geo field for geographic routing'
                        )}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        disabled={sensitiveLocked}
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
            </>
          )}

          {CLAUDE_FIELD_PASSTHROUGH_TYPES.has(currentType) && (
            <>
              {currentType === 14 && (
                <FormField
                  control={form.control}
                  name='allow_inference_geo'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                      <div className='space-y-0.5'>
                        <FormLabel className='text-sm'>
                          {t('Allow inference_geo passthrough')}
                        </FormLabel>
                        <FormDescription>
                          {t(
                            'Pass through the inference_geo field for Claude data residency region control'
                          )}
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          disabled={sensitiveLocked}
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />
              )}

              <FormField
                control={form.control}
                name='allow_speed'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                    <div className='space-y-0.5'>
                      <FormLabel className='text-sm'>
                        {t('Allow speed passthrough')}
                      </FormLabel>
                      <FormDescription>
                        {t(
                          'Pass through the speed field for Claude inference speed mode control'
                        )}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        disabled={sensitiveLocked}
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />

              {currentType === 14 && (
                <FormField
                  control={form.control}
                  name='claude_beta_query'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                      <div className='space-y-0.5'>
                        <FormLabel className='text-sm'>
                          {t('Allow Claude beta query passthrough')}
                        </FormLabel>
                        <FormDescription>
                          {t(
                            'Pass through the anthropic-beta header for beta features'
                          )}
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          disabled={sensitiveLocked}
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />
              )}
            </>
          )}
        </div>
      </fieldset>
    </div>
  )

  const modelsSection = (
    <div className='scroll-mt-4'>
      <ChannelModelsSection>
        <div className='space-y-5'>
          <div className='space-y-2'>
            <div className='space-y-2'>
              <h3 className='text-sm font-semibold'>
                {t('Fetch models from upstream')}
              </h3>
              {MODEL_FETCHABLE_TYPES.has(currentType) &&
                discovery.status === 'error' && (
                  <ErrorState
                    className='mt-4 min-h-0 p-3'
                    title={t('Failed to fetch models')}
                    description={getServerErrorMessage(
                      discovery.error,
                      t('Failed to fetch models')
                    )}
                    onRetry={() => {
                      void handleFetchModels()
                    }}
                  />
                )}
              {MODEL_FETCHABLE_TYPES.has(currentType) &&
                discovery.status !== 'error' && (
                  <div
                    role='status'
                    aria-live='polite'
                    className='border-border/60 bg-muted/20 space-y-3 rounded-lg border p-3'
                  >
                    <div className='flex flex-wrap items-center justify-between gap-2'>
                      <div className='flex min-w-0 items-center gap-2'>
                        {discovery.status === 'loading' && (
                          <Loader2
                            className='text-muted-foreground size-3.5 shrink-0 animate-spin'
                            aria-hidden='true'
                          />
                        )}
                        <span className='text-muted-foreground min-w-0 text-xs'>
                          {discoveryMessage}
                        </span>
                      </div>
                      {canDiscoverModels ? (
                        <div className='flex flex-wrap items-center gap-2'>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={handleFetchModels}
                            disabled={
                              discovery.status === 'loading' ||
                              !discoveryConnectionReady
                            }
                          >
                            <RefreshCw
                              className='mr-1.5 size-3.5'
                              aria-hidden='true'
                            />
                            {discovery.status === 'success' ||
                            discovery.status === 'stale'
                              ? t('Re-fetch')
                              : t('Probe upstream models')}
                          </Button>
                        </div>
                      ) : (
                        <span className='text-muted-foreground text-xs'>
                          {t('No permission to perform this action')}
                        </span>
                      )}
                    </div>
                    {discovery.status === 'success' &&
                      normalizedDiscoveredModels.length === 0 && (
                        <div className='space-y-1'>
                          <p className='text-xs font-medium'>
                            {t('No models returned by the upstream')}
                          </p>
                          <p className='text-muted-foreground text-xs'>
                            {t(
                              'You can add models manually or try fetching again.'
                            )}
                          </p>
                        </div>
                      )}
                    {isEditing && !previewModels && (
                      <p className='text-muted-foreground text-xs'>
                        {t(
                          'Model discovery uses the saved channel connection settings.'
                        )}
                      </p>
                    )}
                    {!isEditing && isBatchMode && (
                      <p className='text-muted-foreground text-xs'>
                        {t(
                          'Model discovery uses the first key; other keys are not tested.'
                        )}
                      </p>
                    )}
                  </div>
                )}
            </div>

            <FormField
              control={form.control}
              name='models'
              render={() => (
                <FormItem
                  role='group'
                  aria-label={t('Models')}
                  className='space-y-3'
                >
                  <div className='flex flex-wrap items-start justify-between gap-2'>
                    <div className='min-w-0 space-y-1'>
                      <FormLabel required>{t('Models')}</FormLabel>
                      <FormDescription>
                        {t(FIELD_DESCRIPTIONS.MODELS)}
                      </FormDescription>
                    </div>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      className='shrink-0'
                      disabled={!fetchModelsDialogFetcher}
                      onClick={() => setFetchModelsDialogOpen(true)}
                    >
                      <ListPlus className='mr-2 h-4 w-4' aria-hidden='true' />
                      {t('Fetch model list')}
                    </Button>
                  </div>
                  <div className='space-y-2'>
                    <span className='text-sm font-medium'>
                      {t('Selected models')}
                    </span>
                    {currentModelsArray.length > 0 ? (
                      <ul className='divide-border/60 divide-y rounded-lg border'>
                        {currentModelsArray.map((model) => (
                          <li
                            key={model}
                            className='flex items-center justify-between gap-2 px-3 py-1.5'
                          >
                            <span className='min-w-0 text-sm wrap-anywhere'>
                              {model}
                            </span>
                            <Button
                              type='button'
                              variant='ghost'
                              size='icon-sm'
                              aria-label={t('Remove {{model}}', { model })}
                              onClick={() => handleRemoveModel(model)}
                            >
                              <X className='size-4' aria-hidden='true' />
                            </Button>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className='text-muted-foreground text-sm'>
                        {t('No models selected')}
                      </p>
                    )}
                  </div>
                  <div className='flex items-center gap-2'>
                    <FormControl>
                      <Input
                        aria-label={t('Add a model manually')}
                        placeholder={t(
                          'Model name (separate multiple with commas or newlines)'
                        )}
                        value={newModelDraft}
                        onChange={(event) =>
                          setNewModelDraft(event.target.value)
                        }
                        onKeyDown={(event) => {
                          if (event.key !== 'Enter') return
                          event.preventDefault()
                          handleAddManualModel()
                        }}
                        onPaste={(event) => {
                          // 单行 input 会丢掉换行：先取剪贴板原文，含分隔符就整批添加。
                          const text =
                            event.clipboardData?.getData('text') ?? ''
                          if (!text.trim() || !/[\s,，、]/.test(text.trim())) {
                            return
                          }
                          event.preventDefault()
                          handleAddManualModel(text)
                        }}
                      />
                    </FormControl>
                    <Button
                      type='button'
                      variant='outline'
                      className='shrink-0'
                      disabled={!newModelDraft.trim()}
                      onClick={() => handleAddManualModel()}
                    >
                      <Plus className='mr-2 h-4 w-4' aria-hidden='true' />
                      {t('Add')}
                    </Button>
                  </div>
                  {modelMappingGuardrail.exposedTargetModels.length > 0 && (
                    <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
                      <AlertDescription className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                        <span>
                          {t('The mapped upstream model(s)')}{' '}
                          {formatModelNames(
                            modelMappingGuardrail.exposedTargetModels
                          )}{' '}
                          {t(
                            'are also listed here. Remove them from Models to keep the `/v1/models` response user-friendly and hide vendor-specific names.'
                          )}
                        </span>
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          onClick={() => {
                            const hiddenTargets = new Set(
                              modelMappingGuardrail.exposedTargetModels
                            )
                            updateModels(
                              currentModelsArray.filter(
                                (model) => !hiddenTargets.has(model)
                              )
                            )
                          }}
                        >
                          {t('Remove mapped targets')}
                        </Button>
                      </AlertDescription>
                    </Alert>
                  )}
                  <FormMessage />
                </FormItem>
              )}
            />

            <Separator className='my-4' />

            <div className='flex flex-wrap items-center gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={handleCopyModels}
                disabled={currentModelsArray.length === 0}
              >
                <Copy className='mr-2 h-4 w-4' aria-hidden='true' />
                {t('Copy All')}
              </Button>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={handleClearModels}
                disabled={currentModelsArray.length === 0}
              >
                <Eraser className='mr-2 h-4 w-4' aria-hidden='true' />
                {t('Clear All')}
              </Button>
            </div>
          </div>
        </div>
      </ChannelModelsSection>
    </div>
  )

  const connectionSection = (
    <div className='scroll-mt-4'>
      <ChannelApiAccessSection>
        {CHANNEL_TYPE_WARNINGS[currentType] && (
          <Alert>
            <AlertDescription>
              {t(CHANNEL_TYPE_WARNINGS[currentType])}
            </AlertDescription>
          </Alert>
        )}

        {sensitiveLocked && (
          <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
            <AlertDescription>
              {t('No permission to perform this action')}
            </AlertDescription>
          </Alert>
        )}

        <div className='border-border/60 bg-muted/10 rounded-lg border p-4'>
          <fieldset
            disabled={sensitiveLocked}
            className='space-y-4 disabled:opacity-60'
          >
            {/* Azure (type 3) */}
            {currentType === 3 && (
              <>
                <FormField
                  control={form.control}
                  name='base_url'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel required>
                        {t('AZURE_OPENAI_ENDPOINT')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t(
                            'e.g., https://docs-test-001.openai.azure.com'
                          )}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Your Azure OpenAI endpoint URL')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='other'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel required>{t('Default API Version')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('e.g., 2025-04-01-preview')}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Default API version for this channel')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='azure_responses_version'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Responses API Version')}</FormLabel>
                      <FormControl>
                        <Input placeholder={t('e.g., preview')} {...field} />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Default Responses API version, if empty, will use the API version above'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </>
            )}

            {/* Custom (type 8) */}
            {currentType === 8 && (
              <FormField
                control={form.control}
                name='base_url'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel required>
                      {t(
                        'Base URL (supports {model} variable, or fill to /v1 to auto-complete)'
                      )}
                    </FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t(
                          'e.g., https://api.openai.com/v1/chat/completions'
                        )}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Enter the full endpoint URL (supports the {model} variable); if it only goes up to a version segment (e.g. https://host/v1), the gateway appends /chat/completions automatically'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {/* Xunfei/Spark (type 18) */}
            {currentType === 18 && (
              <FormField
                control={form.control}
                name='other'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel required>{t('Model Version')}</FormLabel>
                    <FormControl>
                      <Input placeholder={t('e.g., v2.1')} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Spark model version, e.g., v2.1 (version number in API URL)'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {/* OpenRouter (type 20) */}
            {currentType === 20 && (
              <FormField
                control={form.control}
                name='is_enterprise_account'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between'>
                    <div className='space-y-0.5'>
                      <FormLabel>{t('Enterprise Account')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Enable if this is an OpenRouter enterprise account with special response format'
                        )}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        disabled={sensitiveLocked}
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
            )}

            {/* AWS (type 33) */}
            {currentType === 33 && (
              <FormField
                control={form.control}
                name='aws_key_type'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('AWS Key Format')}</FormLabel>
                    <Select
                      disabled={sensitiveLocked}
                      items={[
                        {
                          value: 'ak_sk',
                          label: t('AccessKey / SecretAccessKey'),
                        },
                        {
                          value: 'api_key',
                          label: t('API Key'),
                        },
                      ]}
                      onValueChange={field.onChange}
                      value={field.value}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue placeholder={t('Select key format')} />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectItem value='ak_sk'>
                            {t('AccessKey / SecretAccessKey')}
                          </SelectItem>
                          <SelectItem value='api_key'>
                            {t('API Key')}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {field.value === 'api_key'
                        ? t('API Key mode: use APIKey|Region')
                        : t('AK/SK mode: use AccessKey|SecretAccessKey|Region')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {/* AI Proxy Library (type 21) */}
            {currentType === 21 && (
              <FormField
                control={form.control}
                name='other'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel required>{t('Knowledge Base ID')}</FormLabel>
                    <FormControl>
                      <Input placeholder={t('e.g., 123456')} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t('Enter the knowledge base ID')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {/* FastGPT (type 22) */}
            {currentType === 22 && (
              <FormField
                control={form.control}
                name='base_url'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Private Deployment URL')}</FormLabel>
                    <FormControl>
                      <Input placeholder={baseUrlPlaceholder} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'For private deployments, format: https://fastgpt.run/api/openapi'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {/* SunoAPI (type 36) */}
            {currentType === 36 && (
              <FormField
                control={form.control}
                name='base_url'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel required>
                      {t('API Base URL (Important: Not Chat API)')}
                    </FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t(
                          'e.g., https://api.example.com (path before /suno)'
                        )}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Enter the path before /suno, usually just the domain'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {/* Cloudflare Workers AI (type 39) */}
            {currentType === 39 && (
              <FormField
                control={form.control}
                name='other'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel required>{t('Account ID')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('e.g., d6b5da8hk1awo8nap34ube6gh')}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Your Cloudflare Account ID')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {/* SiliconFlow (type 40) */}
            {currentType === 40 && (
              <Alert>
                <AlertDescription>
                  {t('Referral link:')}{' '}
                  <a
                    href='https://cloud.siliconflow.cn/i/hij0YNTZ'
                    target='_blank'
                    rel='noopener noreferrer'
                    className='text-primary underline'
                  >
                    {t('https://cloud.siliconflow.cn/i/hij0YNTZ')}
                  </a>
                </AlertDescription>
              </Alert>
            )}

            {/* Vertex AI (type 41) */}
            {currentType === 41 && (
              <>
                <FormField
                  control={form.control}
                  name='vertex_key_type'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Vertex AI Key Format')}</FormLabel>
                      <Select
                        disabled={sensitiveLocked}
                        items={[
                          { value: 'json', label: t('JSON') },
                          {
                            value: 'api_key',
                            label: t('API Key'),
                          },
                        ]}
                        onValueChange={field.onChange}
                        value={field.value}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            <SelectItem value='json'>{t('JSON')}</SelectItem>
                            <SelectItem value='api_key'>
                              {t('API Key')}
                            </SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormDescription>
                        {field.value === 'json'
                          ? t('JSON format supports service account JSON files')
                          : t('API Key mode (does not support batch creation)')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                {vertexKeyType === 'json' && (
                  <FormItem>
                    <FormLabel>{t('Service account JSON file(s)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='file'
                        accept='.json,application/json'
                        multiple={isBatchMode}
                        onChange={async (e) => {
                          const fileList = e.target.files
                          const files = fileList ? [...fileList] : []
                          // allow re-selecting the same file
                          e.target.value = ''

                          if (files.length === 0) {
                            toast.info(t('Please upload key file(s)'))
                            return
                          }

                          const keys: unknown[] = []
                          for (const file of files) {
                            try {
                              const txt = await file.text()
                              keys.push(JSON.parse(txt))
                            } catch {
                              toast.error(
                                t('Failed to parse JSON file: {{name}}', {
                                  name: file.name,
                                })
                              )
                              return
                            }
                          }

                          if (keys.length === 0) {
                            toast.info(t('Please upload key file(s)'))
                            return
                          }

                          const keyValue = isBatchMode
                            ? JSON.stringify(keys)
                            : JSON.stringify(keys[0])

                          form.setValue('key', keyValue, {
                            shouldDirty: true,
                            shouldValidate: true,
                          })

                          toast.success(
                            t('Parsed {{count}} service account file(s)', {
                              count: keys.length,
                            })
                          )
                        }}
                      />
                    </FormControl>
                    <FormDescription>
                      {isBatchMode
                        ? t('Upload multiple JSON files in batch modes')
                        : t('Upload a single service account JSON file')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
                <FormField
                  control={form.control}
                  name='other'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel required>{t('Deployment Region')}</FormLabel>
                      <FormControl>
                        <Textarea
                          placeholder={t(
                            'e.g., us-central1 or JSON format for model-specific regions'
                          )}
                          rows={3}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Enter deployment region or JSON mapping:')} {'{'}
                        {t(
                          '"default": "us-central1", "claude-3-5-sonnet-20240620": "europe-west1"'
                        )}
                        {'}'}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </>
            )}

            {/* VolcEngine (type 45) */}
            {currentType === 45 && !doubaoApiEditUnlocked && (
              <FormField
                control={form.control}
                name='base_url'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel
                      required
                      className='cursor-pointer select-none'
                      onClick={handleApiConfigSecretClick}
                    >
                      {t('API Base URL')}
                    </FormLabel>
                    <Select
                      disabled={sensitiveLocked}
                      items={[
                        {
                          value: 'https://ark.cn-beijing.volces.com',
                          label: t('https://ark.cn-beijing.volces.com'),
                        },
                        {
                          value: 'https://ark.ap-southeast.bytepluses.com',
                          label: t('https://ark.ap-southeast.bytepluses.com'),
                        },
                      ]}
                      onValueChange={field.onChange}
                      value={
                        field.value === 'doubao-coding-plan'
                          ? 'https://ark.cn-beijing.volces.com'
                          : field.value || 'https://ark.cn-beijing.volces.com'
                      }
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectItem value='https://ark.cn-beijing.volces.com'>
                            {t('https://ark.cn-beijing.volces.com')}
                          </SelectItem>
                          <SelectItem value='https://ark.ap-southeast.bytepluses.com'>
                            {t('https://ark.ap-southeast.bytepluses.com')}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {t('Select the API endpoint region')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {/* VolcEngine (type 45) - Custom API URL (unlocked) */}
            {currentType === 45 && doubaoApiEditUnlocked && (
              <FormField
                control={form.control}
                name='base_url'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel required>{t('API Base URL')}</FormLabel>
                    <FormControl>
                      <Input placeholder={baseUrlPlaceholder} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t('Enter custom API endpoint URL')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {/* Coze (type 49) */}
            {currentType === 49 && (
              <FormField
                control={form.control}
                name='other'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel required>{t('Agent ID')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('e.g., 7342866812345')}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Enter the Coze agent ID')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {/* General base_url for other types */}
            {![3, 8, 22, 36, 45].includes(currentType) && (
              <FormField
                control={form.control}
                name='base_url'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t(
                        'Base URL (fill to /v1 and the gateway completes the rest)'
                      )}
                    </FormLabel>
                    <FormControl>
                      <Input placeholder={baseUrlPlaceholder} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Fill up to the version segment — https://host/v1 for OpenAI/Anthropic, https://host/v1beta for Gemini. The gateway appends the protocol path (chat/completions, responses, messages, generateContent) automatically. Leave empty to use the provider default. Both with and without the version segment work.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {currentType === CHANNEL_TYPE_ADVANCED_CUSTOM && (
              <FormField
                control={form.control}
                name='advanced_custom'
                render={({ field }) => (
                  <FormItem className='space-y-3 border-y py-4'>
                    <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
                      <div className='space-y-2'>
                        <FormLabel>{t('Advanced Custom Routes')}</FormLabel>
                        <div className='flex flex-wrap gap-2'>
                          <Badge variant='secondary'>
                            {t('Routes')}: {advancedCustomStats.routeCount}
                          </Badge>
                          {advancedCustomRouteTypeLabels.map((label) => (
                            <Badge
                              key={label}
                              variant='outline'
                              className='max-w-[12rem]'
                              title={label}
                            >
                              <span className='truncate'>{label}</span>
                            </Badge>
                          ))}
                          {hiddenAdvancedCustomRouteTypeCount > 0 && (
                            <Badge
                              variant='outline'
                              title={advancedCustomRouteTypeTitle}
                            >
                              +{hiddenAdvancedCustomRouteTypeCount}
                            </Badge>
                          )}
                          {!advancedCustomStats.valid && (
                            <Badge variant='destructive'>
                              {t('Incomplete')}
                            </Badge>
                          )}
                        </div>
                      </div>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() => setAdvancedCustomEditorOpen(true)}
                      >
                        <Route className='mr-2 h-4 w-4' />
                        {t('Configure routes')}
                      </Button>
                    </div>
                    <FormControl>
                      <input type='hidden' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            <ChannelAuthSection>
              {!isEditing && (
                <FormField
                  control={form.control}
                  name='multi_key_mode'
                  render={({ field }) => (
                    <FormItem className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                      <FormLabel className='text-muted-foreground text-xs font-medium'>
                        {t('Add Mode')}
                      </FormLabel>
                      <Select
                        disabled={sensitiveLocked}
                        items={addModeOptions.map((option) => ({
                          value: option.value,
                          label: t(option.label),
                        }))}
                        onValueChange={field.onChange}
                        value={field.value}
                      >
                        <FormControl>
                          <SelectTrigger size='sm' className='w-full sm:w-56'>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {addModeOptions.map((option) => (
                              <SelectItem
                                key={option.value}
                                value={option.value}
                              >
                                {t(option.label)}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}

              <FormField
                control={form.control}
                name='key'
                render={({ field }) => {
                  let keyPlaceholder = t(getKeyPromptForType(currentType))
                  if (isEditing) {
                    keyPlaceholder = t('Leave empty to keep existing key')
                  } else if (
                    currentType === 33 &&
                    awsKeyType === 'api_key' &&
                    isBatchMode
                  ) {
                    keyPlaceholder = t(
                      'Enter API Key, one per line, format: APIKey|Region'
                    )
                  } else if (currentType === 33 && awsKeyType === 'api_key') {
                    keyPlaceholder = t('Enter API Key, format: APIKey|Region')
                  } else if (currentType === 33 && isBatchMode) {
                    keyPlaceholder = t(
                      'Enter key, one per line, format: AccessKey|SecretAccessKey|Region'
                    )
                  } else if (currentType === 33) {
                    keyPlaceholder = t(
                      'Enter key, format: AccessKey|SecretAccessKey|Region'
                    )
                  } else if (isBatchMode) {
                    keyPlaceholder = t(
                      'Enter one key per line for batch creation'
                    )
                  }

                  let keyDescription: ReactNode = t(FIELD_DESCRIPTIONS.KEY)
                  if (isEditing) {
                    let keyModeDescription = t(
                      'Append mode: New keys will be added to the end of the existing key list'
                    )
                    if (keyMode === 'replace') {
                      keyModeDescription = t(
                        'Replace mode: Will completely replace all existing keys'
                      )
                    }
                    keyDescription = (
                      <>
                        {t(
                          'Enter new key to update, or leave empty to keep current key'
                        )}
                        {isMultiKeyChannel && (
                          <span className='text-warning mt-1 block'>
                            {keyModeDescription}
                          </span>
                        )}
                      </>
                    )
                  } else if (isBatchMode) {
                    keyDescription = t(
                      'Enter one API key per line for batch creation'
                    )
                  }
                  return (
                    <FormItem>
                      <FormLabel required>{t('API Key')}</FormLabel>
                      <FormControl>
                        <Textarea
                          placeholder={keyPlaceholder}
                          rows={isBatchMode ? 8 : 4}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        <span className='flex flex-col gap-2'>
                          <span>{keyDescription}</span>
                          {!isEditing && isBatchMode && (
                            <Button
                              type='button'
                              variant='outline'
                              size='sm'
                              onClick={handleDeduplicateKeys}
                              className='w-fit'
                            >
                              <Trash2 className='mr-2 h-4 w-4' />
                              {t('Remove Duplicates')}
                            </Button>
                          )}
                        </span>
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )
                }}
              />

              {currentType === 57 && (
                <div className='border-border/60 flex flex-col gap-3 border-y py-4'>
                  <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                    <div className='text-muted-foreground text-xs'>
                      {t(
                        'Codex channels use an OAuth JSON credential as the key.'
                      )}
                    </div>
                    <div className='flex flex-wrap items-center gap-2'>
                      {isEditing && channelId && (
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          onClick={handleRefreshCodexCredential}
                          disabled={
                            sensitiveLocked || isCodexCredentialRefreshing
                          }
                        >
                          {isCodexCredentialRefreshing ? (
                            <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                          ) : (
                            <RefreshCw className='mr-2 h-4 w-4' />
                          )}
                          {isCodexCredentialRefreshing
                            ? t('Refreshing...')
                            : t('Refresh credential')}
                        </Button>
                      )}
                    </div>
                  </div>
                  <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
                    <AlertDescription>
                      {t(
                        "Disclaimer: Personal use only. Do not distribute or share any credentials. This channel has prerequisites and requires prior setup; use it only if you understand the flow and risks, and comply with OpenAI's terms and policies. Credentials and configuration are for Codex CLI integration only, and are not intended for any other client, platform, or channel."
                      )}
                    </AlertDescription>
                  </Alert>
                </div>
              )}

              {isEditing && isMultiKeyChannel && (
                <FormField
                  control={form.control}
                  name='key_mode'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Key Update Mode')}</FormLabel>
                      <Select
                        disabled={sensitiveLocked}
                        items={[
                          {
                            value: 'append',
                            label: t('Append to existing keys'),
                          },
                          {
                            value: 'replace',
                            label: t('Replace all existing keys'),
                          },
                        ]}
                        onValueChange={field.onChange}
                        value={field.value}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            <SelectItem value='append'>
                              {t('Append to existing keys')}
                            </SelectItem>
                            <SelectItem value='replace'>
                              {t('Replace all existing keys')}
                            </SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormDescription>
                        {field.value === 'replace'
                          ? t(
                              'Replace mode: Will completely replace all existing keys'
                            )
                          : t(
                              'Append mode: New keys will be added to the end of the existing key list'
                            )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}

              {(isMultiKeyChannel ||
                (!isEditing && multiKeyMode === 'multi_to_single')) && (
                <FormField
                  control={form.control}
                  name='multi_key_type'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Multi-Key Strategy')}</FormLabel>
                      <Select
                        disabled={sensitiveLocked}
                        items={[
                          {
                            value: 'random',
                            label: t('Random'),
                          },
                          {
                            value: 'polling',
                            label: t('Polling'),
                          },
                        ]}
                        onValueChange={field.onChange}
                        value={field.value}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            <SelectItem value='random'>
                              {t('Random')}
                            </SelectItem>
                            <SelectItem value='polling'>
                              {t('Polling')}
                            </SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormDescription>
                        {multiKeyType === 'polling' ? (
                          <span className='text-warning'>
                            {t(
                              'Polling mode requires Redis and memory cache, otherwise performance will be significantly degraded'
                            )}
                          </span>
                        ) : (
                          t(
                            'Randomly select a key from the pool for each request'
                          )
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
            </ChannelAuthSection>
          </fieldset>
        </div>
      </ChannelApiAccessSection>
    </div>
  )

  let formContent: ReactNode
  if (isEditing && isChannelError && !channelData) {
    formContent = (
      <ErrorState
        title={t('Failed to load channel')}
        description={getServerErrorMessage(
          channelError,
          t('Failed to load channel')
        )}
        onRetry={() => {
          void refetchChannel()
        }}
      />
    )
  } else if (isChannelDetailLoading) {
    formContent = <ChannelEditorLoadingState />
  } else {
    formContent = (
      <ChannelConfiguration
        section={configurationSection}
        onSectionChange={setConfigurationSection}
        statuses={configuration.sections}
        connection={
          <>
            {basicSection}
            {connectionSection}
          </>
        }
        models={modelsSection}
        routing={
          <>
            {modelMappingFields}
            {routingFields}
          </>
        }
        prices={
          <div className='border-border/60 bg-muted/10 rounded-lg border p-4'>
            {pbrPricesFields}
          </div>
        }
        other={
          <>
            <div
              role='group'
              aria-label={t('Request processing')}
              className={channelConfigurationBlockClassName(
                configuration.blocks.requestProcessing,
                'space-y-4'
              )}
            >
              <CardHeading
                status={configuration.blocks.requestProcessing}
                title={t('Request processing')}
                icon={<Settings className='size-4' />}
              />
              <fieldset
                disabled={sensitiveLocked}
                className='space-y-4 disabled:opacity-60'
              >
                {formatFields}
                {thinkingFields}
                {passthroughFields}
                {systemPromptFields}
                {systemPromptOverrideFields}
              </fieldset>
            </div>
            {overrideFields}
            {fieldPassthroughFields}
            <div
              role='group'
              aria-label={t('Channel Extra Settings')}
              className={channelConfigurationBlockClassName(
                configuration.blocks.extraSettings,
                'space-y-4'
              )}
            >
              <CardHeading
                status={configuration.blocks.extraSettings}
                title={t('Channel Extra Settings')}
                icon={<Settings className='size-4' />}
              />
              <fieldset
                disabled={sensitiveLocked}
                className='space-y-4 disabled:opacity-60'
              >
                {proxyFields}
                {httpProtocolFields}
                {httpShardsFields}
              </fieldset>
            </div>
            {upstreamModelDetectionFields}
            {notesFields}
          </>
        }
      />
    )
  }
  let description = t(
    'Configure the connection and models, then create the channel.'
  )
  if (isEditing) {
    description = t(
      "Update channel configuration and click save when you're done."
    )
  }

  return (
    <>
      <DialogRoot open={open} onOpenChange={handleOpenChange}>
        <DialogContent
          className={cn(
            'flex w-full flex-col gap-4 overflow-hidden p-4 sm:max-w-none sm:p-6',
            DIALOG_SIZE_CLASS.lg
          )}
        >
          <DialogHeader className='pr-12'>
            <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
              <div className='min-w-0 flex-1'>
                <div className='flex min-w-0 items-center gap-2 sm:gap-3'>
                  <DialogTitle className='flex shrink-0 items-center gap-2 sm:gap-3'>
                    <IconBadge tone='info' size='title'>
                      <Server className='size-5' />
                    </IconBadge>
                    <span>
                      {isEditing ? t('Edit Channel') : t('Create Channel')}
                    </span>
                  </DialogTitle>
                </div>
                {isEditing && channelData && (
                  <Badge variant='secondary' className='mt-2'>
                    {t(
                      CHANNEL_STATUS_LABELS[
                        currentStatus as keyof typeof CHANNEL_STATUS_LABELS
                      ] || 'Unknown'
                    )}
                  </Badge>
                )}
                <DialogDescription className='mt-1'>
                  {description}
                </DialogDescription>
              </div>
              {!isEditing && (
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  className='shrink-0'
                  onClick={pasteConnectionInfoFromClipboard}
                >
                  <ClipboardPaste className='size-4' />
                  <span>{t('Paste Connection Info')}</span>
                </Button>
              )}
            </div>
          </DialogHeader>

          {sensitiveLocked && (
            <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
              <AlertDescription>
                {t(
                  'Sensitive channel settings are read-only for your account.'
                )}{' '}
                {t(
                  'You can still edit non-sensitive operations fields such as models and groups.'
                )}
              </AlertDescription>
            </Alert>
          )}

          {!isEditing && clipboardConnectionInfo && (
            <Alert>
              <AlertDescription className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                <span>{t('Connection info detected in clipboard')}</span>
                <span className='flex shrink-0 gap-2'>
                  <Button
                    type='button'
                    size='sm'
                    onClick={() => applyConnectionInfo(clipboardConnectionInfo)}
                  >
                    {t('Fill in')}
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    onClick={() => setClipboardConnectionInfo(null)}
                  >
                    {t('Ignore')}
                  </Button>
                </span>
              </AlertDescription>
            </Alert>
          )}

          <Form {...form}>
            <form
              id='channel-form'
              ref={channelFormRef}
              onSubmit={form.handleSubmit(onSubmit, onInvalid)}
              className='flex min-h-0 flex-1 flex-col gap-5 overflow-hidden'
            >
              {formContent}
            </form>
          </Form>

          <DialogFooter className='mt-2'>
            <Button
              type='button'
              variant='outline'
              disabled={isSubmitting}
              onClick={() => handleOpenChange(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              form='channel-form'
              type='submit'
              disabled={
                isSubmitting ||
                (!isEditing && !canEditSensitive) ||
                (isEditing && !channelData)
              }
            >
              {isSubmitting && (
                <Loader2 className='mr-2 h-4 w-4 animate-spin' />
              )}
              {isEditing ? t('Update Channel') : t('Create Channel')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </DialogRoot>

      {open && modelDiscoveryDialogOpen && (
        <ConfigureModelsDialog
          open
          candidates={normalizedDiscoveredModels}
          selectedModels={currentModelsArray}
          existingModels={
            isEditing ? initialModelsRef.current : currentModelsArray
          }
          redirectModels={redirectModelList}
          redirectSourceModels={redirectModelKeyList}
          onOpenChange={setModelDiscoveryDialogOpen}
          onApply={handleModelDiscoveryApply}
        />
      )}

      {open && (
        <FetchModelsDialog
          open={fetchModelsDialogOpen}
          onOpenChange={setFetchModelsDialogOpen}
          customFetcher={fetchModelsDialogFetcher}
          onModelsSelected={handleFetchModelsToForm}
          channelName={channelData?.name ?? formValues.name}
          existingModels={
            isEditing ? initialModelsRef.current : currentModelsArray
          }
          redirectModels={redirectModelList}
          redirectSourceModels={redirectModelKeyList}
        />
      )}

      {paramOverrideEditorOpen && !sensitiveLocked && (
        <ParamOverrideEditorDialog
          open={paramOverrideEditorOpen}
          value={formValues.param_override || ''}
          onOpenChange={setParamOverrideEditorOpen}
          onSave={(nextValue) => {
            form.setValue('param_override', nextValue, {
              shouldDirty: true,
              shouldValidate: true,
            })
          }}
        />
      )}

      {advancedCustomEditorOpen && !sensitiveLocked && (
        <AdvancedCustomEditorDialog
          open={advancedCustomEditorOpen}
          value={formValues.advanced_custom || ''}
          onOpenChange={setAdvancedCustomEditorOpen}
          onSave={(nextValue) => {
            form.setValue('advanced_custom', nextValue, {
              shouldDirty: true,
              shouldValidate: true,
            })
          }}
        />
      )}

      {/* Missing Models Confirmation Dialog */}
      <MissingModelsConfirmationDialog
        open={missingModelsDialogOpen}
        missingModels={missingModelsList}
        onConfirm={handleMissingModelsAction}
        onOpenChange={setMissingModelsDialogOpen}
      />

      <StatusCodeRiskDialog
        open={statusCodeRiskOpen}
        onOpenChange={(v) => {
          if (!v) handleStatusCodeRiskAction(false)
        }}
        detailItems={statusCodeRiskDetailItems}
        onConfirm={() => handleStatusCodeRiskAction(true)}
      />

      {/* 移除模型仍被车道引用时的清理确认（PBR 车道守卫） */}
      <ConfirmDialog
        open={referencedModelsDialogOpen}
        onOpenChange={(v) => {
          if (!v) handleReferencedModelsAction(false)
        }}
        title={t('Models still referenced by lanes')}
        desc={
          <div className='space-y-2'>
            <p>
              {t('These models are still referenced by the following lanes:')}
            </p>
            <ul className='list-disc space-y-1 ps-5'>
              {referencedModelsLanes.map((lane) => (
                <li key={lane}>{lane}</li>
              ))}
            </ul>
            <p>
              {t(
                'If you continue, this channel will be removed from those lanes and empty lanes will be deleted. Those models become uncallable until a new lane member is configured.'
              )}
            </p>
          </div>
        }
        confirmText={t('Remove and save')}
        destructive
        handleConfirm={() => handleReferencedModelsAction(true)}
      />

      {/* Clear All 是草稿操作，先确认并说明保存后才生效 */}
      <ConfirmDialog
        open={clearModelsConfirmOpen}
        onOpenChange={setClearModelsConfirmOpen}
        title={t('Clear all models?')}
        desc={t(
          'This only clears the draft model list. The channel is not updated until you save.'
        )}
        confirmText={t('Clear draft')}
        handleConfirm={handleConfirmClearModels}
      />
    </>
  )
}
