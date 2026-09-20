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
/* eslint-disable react-refresh/only-export-components */
import type { ColumnDef } from '@tanstack/react-table'
import {
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  ListOrdered,
  Shuffle,
  SlidersHorizontal,
} from 'lucide-react'
import { useState, useMemo, useContext } from 'react'
import { useTranslation } from 'react-i18next'

import { BadgeListCell } from '@/components/data-table'
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import { TruncatedText } from '@/components/truncated-text'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { toIntlLocale } from '@/i18n/languages'
import {
  formatCurrencyFromUSD,
  formatQuotaWithCurrency,
  getCurrencyLabel,
} from '@/lib/currency'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { truncateText } from '@/lib/utils'

import { getCodexUsage } from '../api'
import {
  CHANNEL_PROTOCOL_CUSTOM_VALUE,
  CHANNEL_STATUS_CONFIG,
  MODEL_FETCHABLE_TYPES,
} from '../constants'
import {
  formatRelativeTime,
  formatResponseTime,
  getBalanceVariant,
  getChannelProtocolFromChannel,
  getChannelProtocolLabelKey,
  getResponseTimeConfig,
  isMultiKeyChannel,
  parseModelsList,
  parseChannelSettings,
  isTagAggregateRow,
  type TagRow,
} from '../lib'
import { parseUpstreamUpdateMeta } from '../lib/upstream-update-utils'
import type { Channel } from '../types'
import { ChannelRowActionsLayoutContext } from './channel-row-actions-context'
import { useChannels } from './channels-provider'
import { DataTableRowActions } from './data-table-row-actions'
import { DataTableTagRowActions } from './data-table-tag-row-actions'
import {
  CodexUsageDialog,
  type CodexUsageDialogData,
} from './dialogs/codex-usage-dialog'

/**
 * Upstream update tags (+N / -N) shown on channel name for model-fetchable channels
 */
function UpstreamUpdateTags({ channel }: { channel: Channel }) {
  const { upstream, setCurrentRow } = useChannels()
  if (!MODEL_FETCHABLE_TYPES.has(channel.type)) {
    return null
  }

  const meta = parseUpstreamUpdateMeta(channel.settings)
  if (!meta.enabled) {
    return null
  }

  const addCount = meta.pendingAddModels.length
  const removeCount = meta.pendingRemoveModels.length
  if (addCount === 0 && removeCount === 0) {
    return null
  }

  return (
    <div className='flex items-center gap-0.5'>
      {addCount > 0 && (
        <StatusBadge
          label={`+${addCount}`}
          variant='success'
          size='sm'
          copyable={false}
          className='cursor-pointer'
          onClick={(e: React.MouseEvent) => {
            e.stopPropagation()
            setCurrentRow(channel)
            upstream.openModal(
              channel,
              meta.pendingAddModels,
              meta.pendingRemoveModels,
              'add'
            )
          }}
        />
      )}
      {removeCount > 0 && (
        <StatusBadge
          label={`-${removeCount}`}
          variant='danger'
          size='sm'
          copyable={false}
          className='cursor-pointer'
          onClick={(e: React.MouseEvent) => {
            e.stopPropagation()
            setCurrentRow(channel)
            upstream.openModal(
              channel,
              meta.pendingAddModels,
              meta.pendingRemoveModels,
              'remove'
            )
          }}
        />
      )}
    </div>
  )
}

/**
 * Inline balance/used values longer than this switch to locale-aware compact
 * notation (e.g. "$28万"); the precise value stays available in the tooltip.
 */
const MAX_INLINE_BALANCE_CHARS = 8
const SENSITIVE_MASK = '••••'

/**
 * Protocol cell. Value comes from the single shared normalizer
 * (`getChannelProtocolFromChannel`) that also drives the editor's protocol
 * dropdown, so list and dialog can never disagree (ui-spec §6.4). Channels
 * whose adapter type is outside the 4 protocols show "Custom" instead of being
 * silently rewritten to one of them.
 */
export function ProtocolCell({ channel }: { channel: Channel }) {
  const { t } = useTranslation()

  if (isTagAggregateRow(channel)) {
    return <span className='text-muted-foreground text-xs'>-</span>
  }

  const protocol = getChannelProtocolFromChannel(channel)
  const labelKey = getChannelProtocolLabelKey(protocol)

  return (
    <div className='flex min-w-0 items-center gap-1.5'>
      <TruncatedText text={t(labelKey)} maxWidth='max-w-full' />
    </div>
  )
}

/**
 * Second line of the merged Response column: when the channel was last probed.
 * The absolute timestamp stays in the tooltip so the narrow cell can show a
 * scannable relative time.
 */
function ResponseTestedTime(props: {
  testTime: number
  locale: Intl.LocalesArgument
}) {
  if (!props.testTime || props.testTime === 0) {
    return <span className='text-muted-foreground text-xs'>-</span>
  }

  const fullDate = formatTimestampToDate(props.testTime)

  return (
    <TooltipProvider delay={100}>
      <Tooltip>
        <TooltipTrigger
          render={
            <span className='text-muted-foreground w-fit text-xs whitespace-nowrap' />
          }
        >
          {formatRelativeTime(props.testTime, props.locale)}
        </TooltipTrigger>
        <TooltipContent side='top'>
          <p className='font-mono text-sm'>{fullDate}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

/**
 * Balance/usage cell. Codex channels open their account usage dialog from the
 * remaining badge; other channels render read-only values.
 */
export function BalanceCell({ channel }: { channel: Channel }) {
  const { t, i18n } = useTranslation()
  const layout = useContext(ChannelRowActionsLayoutContext)
  const { sensitiveVisible } = useChannels()
  const isTagRow = isTagAggregateRow(channel)
  const balance = channel.balance || 0
  const usedQuota = channel.used_quota || 0
  const [isUpdating, setIsUpdating] = useState(false)
  const [codexUsageOpen, setCodexUsageOpen] = useState(false)
  const [codexUsageResponse, setCodexUsageResponse] =
    useState<CodexUsageDialogData | null>(null)
  const currencyLabel = getCurrencyLabel()
  const tokenSuffix = currencyLabel === 'Tokens' ? ' Tokens' : ''
  const withSuffix = (value: string) =>
    tokenSuffix && value !== '-' ? `${value}${tokenSuffix}` : value

  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const balanceFormatOptions = {
    digitsLarge: 2,
    digitsSmall: 4,
    abbreviate: false,
    showSymbol: layout !== 'card',
  } as const
  // Precise values are kept for the tooltip; long values are shown compactly inline.
  const usedFull = withSuffix(
    formatQuotaWithCurrency(usedQuota, {
      digitsLarge: 2,
      digitsSmall: 4,
      abbreviate: true,
      showSymbol: layout !== 'card',
    })
  )
  const remainingFull = withSuffix(
    formatCurrencyFromUSD(balance, balanceFormatOptions)
  )
  const usedDisplay =
    usedFull.length > MAX_INLINE_BALANCE_CHARS
      ? withSuffix(
          formatQuotaWithCurrency(usedQuota, {
            compact: true,
            locale,
            showSymbol: layout !== 'card',
          })
        )
      : usedFull
  const remainingDisplay =
    remainingFull.length > MAX_INLINE_BALANCE_CHARS
      ? withSuffix(
          formatCurrencyFromUSD(balance, {
            compact: true,
            locale,
            showSymbol: layout !== 'card',
          })
        )
      : remainingFull
  const usedLabel = `${t('Used:')} ${usedFull}`
  const remainingLabel = `${t('Remaining:')} ${remainingFull}`
  const maskedUsedLabel = `${t('Used:')} ${SENSITIVE_MASK}`
  const maskedRemainingLabel = `${t('Remaining:')} ${SENSITIVE_MASK}`

  // Tag row: only show cumulative used quota
  if (isTagRow) {
    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger
            render={
              <StatusBadge
                label={
                  sensitiveVisible
                    ? `${t('Used:')} ${usedDisplay}`
                    : maskedUsedLabel
                }
                variant='neutral'
                size='sm'
                copyable={false}
                showDot={false}
                className='-ml-1.5 cursor-help'
              />
            }
          />
          <TooltipContent>
            <p>{sensitiveVisible ? usedLabel : maskedUsedLabel}</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )
  }

  // Regular channel row: show used and remaining; Codex channels can open
  // their account usage dialog from the remaining badge.
  const variant = getBalanceVariant(balance)
  const isCodexChannel = channel.type === 57

  const handleOpenCodexUsage = async () => {
    if (!isCodexChannel || isUpdating) {
      return
    }

    setIsUpdating(true)
    try {
      const res = await getCodexUsage(channel.id)
      setCodexUsageResponse(res)
      setCodexUsageOpen(true)
    } catch (error) {
      handleServerError(error, t('Failed to fetch usage'))
    } finally {
      setIsUpdating(false)
    }
  }
  let remainingBadgeLabel = sensitiveVisible ? remainingDisplay : SENSITIVE_MASK
  if (sensitiveVisible && isUpdating) {
    remainingBadgeLabel = t('Updating...')
  } else if (sensitiveVisible && channel.type === 57) {
    remainingBadgeLabel = t('Account Info')
  }
  let remainingTooltipLabel = remainingLabel
  if (!sensitiveVisible) {
    remainingTooltipLabel = maskedRemainingLabel
  } else if (channel.type === 57) {
    remainingTooltipLabel = t('Click to view Codex usage')
  }
  let remainingBadgeVariant: StatusBadgeProps['variant'] = variant
  if (channel.type === 57) {
    remainingBadgeVariant = 'info'
  } else if (isUpdating) {
    remainingBadgeVariant = 'neutral'
  }

  return (
    <TooltipProvider>
      <div className='-ml-1.5 flex items-center gap-1'>
        <Tooltip>
          <TooltipTrigger
            render={
              <StatusBadge
                label={sensitiveVisible ? usedDisplay : SENSITIVE_MASK}
                variant='neutral'
                size='sm'
                copyable={false}
                showDot={false}
                className='cursor-help'
              />
            }
          />
          <TooltipContent>
            <p>{sensitiveVisible ? usedLabel : maskedUsedLabel}</p>
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger
            render={
              <StatusBadge
                label={remainingBadgeLabel}
                variant={remainingBadgeVariant}
                size='sm'
                copyable={false}
                showDot={false}
                className={isCodexChannel ? 'cursor-pointer' : undefined}
                onClick={isCodexChannel ? handleOpenCodexUsage : undefined}
              />
            }
          />
          <TooltipContent>
            <p>{remainingTooltipLabel}</p>
          </TooltipContent>
        </Tooltip>
      </div>

      <CodexUsageDialog
        open={codexUsageOpen}
        onOpenChange={setCodexUsageOpen}
        channelName={channel.name}
        channelId={channel.id}
        channelDisplayName={sensitiveVisible ? undefined : SENSITIVE_MASK}
        channelDisplayId={sensitiveVisible ? undefined : SENSITIVE_MASK}
        response={codexUsageResponse}
        onRefresh={async () => {
          if (isUpdating) {
            return
          }
          setIsUpdating(true)
          try {
            const res = await getCodexUsage(channel.id)
            setCodexUsageResponse(res)
          } catch (error) {
            handleServerError(error, t('Failed to fetch usage'))
          } finally {
            setIsUpdating(false)
          }
        }}
        isRefreshing={isUpdating}
      />
    </TooltipProvider>
  )
}

/**
 * Generate channels columns configuration
 */
export function useChannelsColumns(
  options: {
    enableSelection?: boolean
  } = {}
): ColumnDef<Channel>[] {
  const { t, i18n } = useTranslation()
  const { sensitiveVisible } = useChannels()
  const enableSelection = options.enableSelection ?? true
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  // The column definitions only depend on the translation function, the active
  // locale, and sensitive-data visibility. Memoizing keeps the array (and every
  // cell renderer reference) stable across unrelated re-renders, so react-table
  // does not invalidate the whole row model on each parent render.
  return useMemo<ColumnDef<Channel>[]>(
    () => [
      // Checkbox column
      ...(enableSelection
        ? [
            {
              id: 'select',
              header: ({ table }) => (
                <Checkbox
                  checked={table.getIsAllPageRowsSelected()}
                  indeterminate={table.getIsSomePageRowsSelected()}
                  onCheckedChange={(value) =>
                    table.toggleAllPageRowsSelected(!!value)
                  }
                  aria-label={t('Select all')}
                />
              ),
              cell: ({ row }) => {
                const isTagRow = isTagAggregateRow(row.original)

                // Don't show checkbox for tag rows
                if (isTagRow) {
                  return null
                }

                return (
                  <Checkbox
                    checked={row.getIsSelected()}
                    onCheckedChange={(value) => row.toggleSelected(!!value)}
                    aria-label={t('Select row')}
                  />
                )
              },
              enableSorting: false,
              enableHiding: false,
              enableResizing: false,
              size: 40,
            } satisfies ColumnDef<Channel>,
          ]
        : []),

      // Channel column (name + protocol glyph + multi-key rotation badge)
      {
        accessorKey: 'name',
        header: t('Channel'),
        meta: { mobileTitle: true },
        cell: ({ row }) => {
          const isTagRow = isTagAggregateRow(row.original)
          const name = row.getValue('name') as string
          const channel = row.original

          // Tag row with expand/collapse
          if (isTagRow) {
            const tag = (row.original as TagRow).tag || name
            const childrenCount = (row.original as TagRow).children?.length || 0

            return (
              <div className='flex items-center gap-2'>
                <Button
                  variant='ghost'
                  size='sm'
                  className='h-6 w-6 p-0'
                  onClick={row.getToggleExpandedHandler()}
                >
                  {row.getIsExpanded() ? (
                    <ChevronDown className='h-4 w-4' />
                  ) : (
                    <ChevronRight className='h-4 w-4' />
                  )}
                </Button>
                <div className='flex items-center gap-1.5'>
                  <span className='font-semibold'>Tag：{tag}</span>
                  <StatusBadge
                    label={`${childrenCount} channels`}
                    variant='blue'
                    size='sm'
                    copyable={false}
                  />
                </div>
              </div>
            )
          }

          // Regular channel row
          const settings = parseChannelSettings(channel.setting)
          const isPassThrough = settings.pass_through_body_enabled === true
          const hasParamOverride = Boolean(channel.param_override?.trim())
          // 多密钥轮询模式标记：原在「类型」列，该列移除后移到名称单元格，
          // 避免丢掉"随机/轮询"这一运维信号。
          const isMultiKey = isMultiKeyChannel(channel)
          const multiKeyMode = channel.channel_info?.multi_key_mode ?? 'random'
          const MultiKeyModeIcon =
            multiKeyMode === 'random' ? Shuffle : ListOrdered
          const multiKeyTooltip =
            multiKeyMode === 'random'
              ? t('Multi-key: Random rotation')
              : t('Multi-key: Polling rotation')

          return (
            <div className='flex max-w-full min-w-0 items-center gap-2'>
              <div className='flex max-w-full min-w-0 flex-col gap-1'>
                <div className='flex max-w-full min-w-0 items-center gap-1.5'>
                  <TruncatedText
                    text={sensitiveVisible ? name : SENSITIVE_MASK}
                    className='font-medium'
                    maxWidth='max-w-full'
                  />
                  {isPassThrough && (
                    <TooltipProvider delay={100}>
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <AlertTriangle className='h-3.5 w-3.5 flex-shrink-0 text-amber-500' />
                          }
                        />
                        <TooltipContent side='top'>
                          {t(
                            'Request body pass-through is enabled. The request body will be sent directly to the upstream without any conversion.'
                          )}
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  )}
                  {hasParamOverride && (
                    <TooltipProvider delay={100}>
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <SlidersHorizontal className='text-info h-3.5 w-3.5 flex-shrink-0' />
                          }
                        />
                        <TooltipContent side='top'>
                          {t('Override request parameters')}
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  )}
                  <UpstreamUpdateTags channel={channel} />
                  {isMultiKey && (
                    <TooltipProvider delay={100}>
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <span className='border-border bg-muted text-primary inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-md border' />
                          }
                        >
                          <MultiKeyModeIcon className='h-3 w-3' />
                        </TooltipTrigger>
                        <TooltipContent side='top'>
                          {multiKeyTooltip}
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  )}
                </div>
                {channel.remark && (
                  <TooltipProvider delay={200}>
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <span className='text-muted-foreground text-xs' />
                        }
                      >
                        {truncateText(channel.remark, 40)}
                      </TooltipTrigger>
                      <TooltipContent side='bottom' className='max-w-xs'>
                        {channel.remark}
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                )}
              </div>
            </div>
          )
        },
        size: 260,
        minSize: 200,
      },

      // Protocol column (4 upstream protocols + "Custom" for legacy vendor types)
      {
        id: 'protocol',
        accessorFn: (row) =>
          isTagAggregateRow(row)
            ? undefined
            : (getChannelProtocolFromChannel(row) ??
              CHANNEL_PROTOCOL_CUSTOM_VALUE),
        header: t('Protocol'),
        meta: { mobileHidden: true },
        cell: ({ row }) => <ProtocolCell channel={row.original} />,
        filterFn: (row, id, value) => {
          const selected = value as string[] | undefined
          if (!selected || selected.length === 0) {
            return true
          }

          // Tag rows aggregate channels of mixed protocols; keep them visible so
          // filtering never hides a whole group by accident.
          if (isTagAggregateRow(row.original)) {
            return true
          }

          return selected.includes(String(row.getValue(id) ?? ''))
        },
        size: 170,
        minSize: 140,
        enableSorting: false,
      },

      // Models column (declared count + list summary)
      {
        accessorKey: 'models',
        header: t('Models'),
        meta: { mobileHidden: true },
        cell: ({ row }) => {
          const models = row.getValue('models') as string
          const modelArray = parseModelsList(models)
          return (
            <div className='flex min-w-0 flex-col gap-0.5'>
              <span className='text-muted-foreground text-xs whitespace-nowrap'>
                {t('{{count}} models', { count: modelArray.length })}
              </span>
              <BadgeListCell
                items={modelArray.map((model) => (
                  <StatusBadge
                    key={model}
                    label={model}
                    autoColor={model}
                    size='sm'
                    className='font-mono'
                  />
                ))}
              />
            </div>
          )
        },
        size: 200,
        enableSorting: false,
      },

      // Tag column
      {
        accessorKey: 'tag',
        header: t('Tag'),
        meta: { mobileHidden: true },
        cell: ({ row }) => {
          const tag = row.getValue('tag') as string | null
          if (!tag) {
            return <span className='text-muted-foreground text-xs'>-</span>
          }

          return (
            <StatusBadge
              label={tag}
              autoColor={tag}
              size='sm'
              className='-ml-1.5'
            />
          )
        },
        size: 120,
        enableSorting: false,
      },

      // Status column
      {
        accessorKey: 'status',
        header: t('Status'),
        meta: { mobileBadge: true },
        cell: ({ row }) => {
          const isTagRow = isTagAggregateRow(row.original)
          const status = row.getValue('status') as number
          const channel = row.original as Channel

          // Tag row: show aggregated status
          if (isTagRow) {
            const childrenCount = (row.original as TagRow).children?.length || 0
            const hasEnabled = status === 1

            if (hasEnabled) {
              return (
                <StatusBadge
                  label={`Active (${childrenCount})`}
                  variant='success'
                  size='sm'
                  copyable={false}
                  className='-ml-1.5'
                />
              )
            } else {
              return (
                <StatusBadge
                  label={`Inactive (${childrenCount})`}
                  variant='neutral'
                  size='sm'
                  copyable={false}
                  className='-ml-1.5'
                />
              )
            }
          }

          // Regular channel row
          const config =
            CHANNEL_STATUS_CONFIG[
              status as keyof typeof CHANNEL_STATUS_CONFIG
            ] || CHANNEL_STATUS_CONFIG[0]

          const isMultiKey = isMultiKeyChannel(channel)
          const keySize = channel.channel_info?.multi_key_size ?? 0
          const disabledCount = channel.channel_info?.multi_key_status_list
            ? Object.keys(channel.channel_info.multi_key_status_list).length
            : 0
          const enabledCount = Math.max(0, keySize - disabledCount)
          const label =
            isMultiKey && keySize > 0
              ? `${t(config.label)} (${enabledCount}/${keySize})`
              : t(config.label)

          // Auto-disabled: show reason and time tooltip
          if (status === 3) {
            let statusReason = ''
            let statusTime = ''
            try {
              const otherInfo = channel.other_info
                ? JSON.parse(channel.other_info)
                : null
              if (otherInfo) {
                statusReason = otherInfo.status_reason || ''
                statusTime = otherInfo.status_time
                  ? formatTimestampToDate(otherInfo.status_time)
                  : ''
              }
            } catch {
              /* empty */
            }

            if (statusReason || statusTime) {
              return (
                <TooltipProvider delay={100}>
                  <Tooltip>
                    <TooltipTrigger render={<span />}>
                      <StatusBadge
                        label={label}
                        variant={config.variant}
                        size='sm'
                        copyable={false}
                      />
                    </TooltipTrigger>
                    <TooltipContent side='top' className='max-w-xs'>
                      <div className='space-y-1 text-xs'>
                        {statusReason && (
                          <div>
                            {t('Reason:')} {statusReason}
                          </div>
                        )}
                        {statusTime && (
                          <div>
                            {t('Time:')} {statusTime}
                          </div>
                        )}
                      </div>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              )
            }
          }

          return (
            <StatusBadge
              label={label}
              variant={config.variant}
              size='sm'
              copyable={false}
            />
          )
        },
        filterFn: (row, id, value) => {
          if (!value || value.length === 0 || value.includes('all')) {
            return true
          }
          const status = row.getValue(id) as number
          if (value.includes('enabled')) {
            return status === 1
          }
          if (value.includes('disabled')) {
            return status !== 1
          }
          return false
        },
        size: 120,
        enableSorting: false,
      },

      // Response column (probe latency + last tested time)
      {
        accessorKey: 'response_time',
        header: t('Response'),
        meta: { mobileHidden: true },
        cell: ({ row }) => {
          const responseTime = row.getValue('response_time') as number
          const config = getResponseTimeConfig(responseTime)
          const testTime = row.getValue('test_time') as number

          return (
            <div className='flex min-w-0 flex-col gap-0.5'>
              <StatusBadge
                label={formatResponseTime(responseTime, t)}
                variant={config.variant}
                size='sm'
                copyable={false}
                className='-ml-1.5'
              />
              <ResponseTestedTime testTime={testTime} locale={locale} />
            </div>
          )
        },
        size: 150,
        minSize: 130,
      },

      // Usage column (read-only upstream balance reference; PBR has no billing)
      {
        accessorKey: 'balance',
        header: t('Usage'),
        cell: ({ row }) => <BalanceCell channel={row.original} />,
        size: 180,
      },

      // Created column
      {
        accessorKey: 'created_time',
        header: t('Created'),
        meta: { mobileHidden: true },
        cell: ({ row }) => {
          const createdTime = row.getValue('created_time') as number
          if (!createdTime || createdTime === 0) {
            return <span className='text-muted-foreground text-xs'>-</span>
          }

          return (
            <TruncatedText
              text={formatTimestampToDate(createdTime)}
              maxWidth='max-w-full'
              className='text-muted-foreground text-xs'
            />
          )
        },
        size: 160,
        enableSorting: false,
      },

      // Actions column
      {
        id: 'actions',
        header: () => t('Actions'),
        cell: ({ row }) => {
          // Check if this is a tag row (has children)
          const isTagRow = isTagAggregateRow(row.original)

          if (isTagRow) {
            return (
              <DataTableTagRowActions
                // eslint-disable-next-line @typescript-eslint/no-explicit-any
                row={row as any}
              />
            )
          }

          return <DataTableRowActions row={row} />
        },
        enableSorting: false,
        enableHiding: false,
        meta: { pinned: 'right' as const },
      },
    ],
    [enableSelection, t, locale, sensitiveVisible]
  )
}
