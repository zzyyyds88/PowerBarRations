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
import type { ColumnDef } from '@tanstack/react-table'
import { GitBranch, KeyRound } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { LOG_TYPE_ALL_VALUE } from '../../constants'
import type { UsageLog } from '../../data/schema'
import {
  formatModelName,
  parseLogOther,
  renderAuditContent,
} from '../../lib/format'
import {
  isDisplayableLogType,
  isTimingLogType,
  getLogTypeConfig,
} from '../../lib/utils'
import type { LogOtherData } from '../../types'
import { DetailsDialog } from '../dialogs/details-dialog'
import { ModelBadge } from '../model-badge'
import { TimingMetricsCell, StreamTpsCell } from '../timing-metrics-cell'
import { useUsageLogsContext } from '../usage-logs-provider'

interface DetailSegment {
  text: string
  muted?: boolean
  danger?: boolean
}

/**
 * 通用日志（基座 /api/log/**）只存元数据：时间/类型/模型/渠道/令牌/
 * token/耗时/状态/内容。PBR 没有用户、分组、额度与计费语义，因此详情预览
 * 只挑排障相关的信息，不再渲染倍率/价格/分组/额度。
 */
function buildDetailSegments(
  log: UsageLog,
  other: LogOtherData | null,
  t: (key: string, opts?: Record<string, unknown>) => string,
  isAdmin: boolean
): DetailSegment[] {
  const segments: DetailSegment[] = []

  // 额度钳制是罕见的后端异常标记（admin-only），优先并高亮展示。
  if (isAdmin && other?.admin_info?.quota_saturation) {
    segments.push({ text: t('Quota clamped'), danger: true })
  }

  // 充值/管理/登录日志带本地化的操作描述。
  if (log.type === 1 || log.type === 3 || log.type === 7) {
    const text = renderAuditContent(other, t)
    if (text) segments.push({ text })
    return segments
  }

  // 退款（type=6）：展示原因。
  if (log.type === 6) {
    if (other?.reason) segments.push({ text: other.reason })
    return segments
  }

  if (log.type === 2 && other) {
    if (other.is_system_prompt_overwritten) {
      segments.push({ text: t('System Prompt Override'), danger: true })
    }
    if (other.stream_status && other.stream_status.status !== 'ok') {
      segments.push({
        text:
          t('Stream Status') +
          ': ' +
          (other.stream_status.end_reason || t('Error')),
        danger: true,
      })
    }
  }

  return segments
}

export function useCommonLogsColumns(
  isAdmin: boolean,
  isRoot: boolean
): ColumnDef<UsageLog>[] {
  const { t } = useTranslation()
  const columns: ColumnDef<UsageLog>[] = [
    {
      accessorKey: 'created_at',
      header: t('Time'),
      cell: ({ row }) => {
        const log = row.original
        const timestamp = row.getValue('created_at') as number
        const config = getLogTypeConfig(log.type)

        return (
          <div className='flex min-w-0 flex-col gap-0.5'>
            <span className='truncate font-mono text-xs tabular-nums'>
              {formatTimestampToDate(timestamp)}
            </span>
            <StatusBadge
              label={t(config.label)}
              variant={config.color as StatusBadgeProps['variant']}
              size='sm'
              copyable={false}
              className='-ml-1.5 !text-xs [&_span]:!text-xs'
            />
          </div>
        )
      },
      filterFn: (row, _id, value) => {
        if (!Array.isArray(value) || value.length === 0) return true
        if (value.includes(LOG_TYPE_ALL_VALUE)) return true
        return value.includes(String(row.original.type))
      },
      enableHiding: false,
      size: 180,
    },
  ]

  if (isAdmin) {
    columns.push({
      id: 'channel',
      header: t('Channel'),
      accessorFn: (row) => row.channel,
      cell: function ChannelCell({ row }) {
        const { sensitiveVisible } = useUsageLogsContext()
        const log = row.original

        if (!isDisplayableLogType(log.type)) return null

        const other = parseLogOther(log.other)
        const rawUseChannel = other?.admin_info?.use_channel ?? []
        const useChannel = Array.isArray(rawUseChannel)
          ? rawUseChannel.map(String).filter(Boolean)
          : []
        const hasRetryChain = useChannel.length > 1
        const channelChain = hasRetryChain ? useChannel.join(' → ') : undefined
        const channelDisplay = log.channel_name
          ? log.channel_name + ' #' + log.channel
          : '#' + log.channel
        const channelIdDisplay = '#' + log.channel
        const channelName = sensitiveVisible ? log.channel_name : '••••'
        const multiKeyIndex = other?.admin_info?.multi_key_index
        const showMultiKeyIndex =
          other?.admin_info?.is_multi_key === true &&
          typeof multiKeyIndex === 'number' &&
          Number.isFinite(multiKeyIndex)

        return (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger
                render={<div className='flex max-w-[160px] flex-col gap-0.5' />}
              >
                <div className='relative inline-flex w-fit items-center gap-1'>
                  <StatusBadge
                    label={channelIdDisplay}
                    autoColor={String(log.channel)}
                    copyText={String(log.channel)}
                    size='sm'
                    showDot={false}
                    className='font-mono'
                  />
                  {showMultiKeyIndex && (
                    <StatusBadge
                      label={String(multiKeyIndex)}
                      size='sm'
                      showDot={false}
                      copyable={false}
                      variant='neutral'
                      className='h-5 min-w-5 justify-center rounded-full px-1 font-mono text-xs'
                      aria-label={t('Key') + ' ' + multiKeyIndex}
                    />
                  )}
                  {hasRetryChain && (
                    <Popover>
                      <PopoverTrigger
                        render={
                          <button
                            type='button'
                            className='text-muted-foreground hover:text-foreground focus-visible:ring-ring inline-flex size-5 shrink-0 items-center justify-center rounded-full transition-colors focus-visible:ring-2 focus-visible:outline-none'
                            aria-label={t('Retry Chain')}
                            onClick={(e) => e.stopPropagation()}
                          />
                        }
                      >
                        <GitBranch
                          className='size-3.5 text-amber-500'
                          aria-hidden='true'
                        />
                      </PopoverTrigger>
                      <PopoverContent
                        side='top'
                        align='start'
                        className='w-64 text-xs'
                      >
                        <div className='flex flex-col gap-1'>
                          <p className='font-medium'>{t('Retry Chain')}</p>
                          <p className='text-muted-foreground font-mono break-all'>
                            {channelChain}
                          </p>
                        </div>
                      </PopoverContent>
                    </Popover>
                  )}
                </div>
                {log.channel_name && (
                  <span className='text-muted-foreground/70 truncate [font-family:var(--font-body)] !text-xs'>
                    {channelName}
                  </span>
                )}
              </TooltipTrigger>
              <TooltipContent>
                <div className='space-y-1'>
                  <p>{sensitiveVisible ? channelDisplay : channelIdDisplay}</p>
                  {channelChain && (
                    <p className='text-muted-foreground text-xs'>
                      {t('Chain')}: {channelChain}
                    </p>
                  )}
                  {showMultiKeyIndex && (
                    <p className='text-muted-foreground text-xs'>
                      {t('Key')}: {multiKeyIndex}
                    </p>
                  )}
                </div>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )
      },
    })
  }

  columns.push(
    {
      accessorKey: 'token_name',
      header: t('Token'),
      cell: function TokenNameCell({ row }) {
        const { sensitiveVisible } = useUsageLogsContext()
        const log = row.original
        if (!isDisplayableLogType(log.type)) return null

        const tokenName = log.token_name
        if (!tokenName) return null

        const displayName = sensitiveVisible ? tokenName : '••••'

        return (
          <div className='flex max-w-[200px] flex-col gap-0.5'>
            <TooltipProvider delay={300}>
              <Tooltip>
                <TooltipTrigger render={<div className='max-w-full' />}>
                  <StatusBadge
                    label={displayName}
                    icon={KeyRound}
                    copyText={sensitiveVisible ? tokenName : undefined}
                    size='sm'
                    showDot={false}
                    className='border-border/60 bg-muted/30 text-foreground h-6 max-w-full gap-1.5 overflow-hidden rounded-md border px-2 py-0.5 [font-family:var(--font-body)]'
                  />
                </TooltipTrigger>
                {sensitiveVisible && tokenName.length > 16 && (
                  <TooltipContent side='top' className='max-w-xs break-all'>
                    {tokenName}
                  </TooltipContent>
                )}
              </Tooltip>
            </TooltipProvider>
          </div>
        )
      },
      size: 160,
    },
    {
      accessorKey: 'model_name',
      header: t('Model'),
      cell: function ModelCell({ row }) {
        const log = row.original
        if (!isDisplayableLogType(log.type)) return null

        const modelInfo = formatModelName(log)

        return (
          <div className='flex w-fit flex-col gap-0.5'>
            <ModelBadge
              modelName={modelInfo.name}
              actualModel={modelInfo.actualModel}
            />
          </div>
        )
      },
      meta: { mobileTitle: true },
    },
    {
      accessorKey: 'is_stream',
      header: t('Stream'),
      cell: ({ row }) => {
        const log = row.original
        if (!isTimingLogType(log.type)) return null

        const useTime = row.getValue('use_time') as number
        const other = parseLogOther(log.other)
        const tokensPerSecond =
          useTime > 0 && log.completion_tokens > 0
            ? log.completion_tokens / useTime
            : null

        return (
          <StreamTpsCell
            isStream={log.is_stream}
            tokensPerSecond={tokensPerSecond}
            streamStatus={other?.stream_status}
          />
        )
      },
      meta: { label: t('Stream') },
    },
    {
      accessorKey: 'prompt_tokens',
      header: 'Tokens',
      cell: ({ row }) => {
        const log = row.original
        if (!isDisplayableLogType(log.type)) return null

        const other = parseLogOther(log.other)

        const promptTokens = log.prompt_tokens || 0
        const completionTokens = log.completion_tokens || 0
        if (promptTokens === 0 && completionTokens === 0) {
          return <span className='text-muted-foreground text-xs'>-</span>
        }

        const cacheReadTokens = other?.cache_tokens || 0
        const cacheWrite5m = other?.cache_creation_tokens_5m || 0
        const cacheWrite1h = other?.cache_creation_tokens_1h || 0
        const hasSplitCache = cacheWrite5m > 0 || cacheWrite1h > 0
        const cacheWriteTokens = hasSplitCache
          ? cacheWrite5m + cacheWrite1h
          : other?.cache_creation_tokens || 0

        return (
          <div className='flex flex-col gap-0.5'>
            <span className='font-mono text-xs font-medium tabular-nums'>
              {promptTokens.toLocaleString()} /{' '}
              {completionTokens.toLocaleString()}
            </span>
            {(cacheReadTokens > 0 || cacheWriteTokens > 0) && (
              <div className='flex items-center gap-1 text-[11px]'>
                {cacheReadTokens > 0 && (
                  <span className='text-muted-foreground/60'>
                    {t('Cache')}↓ {cacheReadTokens.toLocaleString()}
                  </span>
                )}
                {cacheWriteTokens > 0 && (
                  <span className='text-muted-foreground/60'>
                    ↑ {cacheWriteTokens.toLocaleString()}
                  </span>
                )}
              </div>
            )}
          </div>
        )
      },
    },
    {
      accessorKey: 'use_time',
      header: t('Timing'),
      cell: ({ row }) => {
        const log = row.original
        if (!isTimingLogType(log.type)) return null

        const useTime = row.getValue('use_time') as number
        const other = parseLogOther(log.other)

        return (
          <TimingMetricsCell
            useTimeSec={useTime}
            completionTokens={log.completion_tokens}
            frtMs={other?.frt}
            isStream={log.is_stream}
          />
        )
      },
    },
    {
      accessorKey: 'content',
      header: t('Details'),
      cell: function DetailsCell({ row }) {
        const { t } = useTranslation()
        const [dialogOpen, setDialogOpen] = useState(false)
        const log = row.original
        const other = parseLogOther(log.other)

        const segments = buildDetailSegments(log, other, t, isAdmin)
        const primary = segments[0]
        const hasMore = segments.length > 1
        let primaryTextClass = 'text-foreground'
        if (primary?.muted) {
          primaryTextClass = 'text-muted-foreground/60'
        } else if (primary?.danger) {
          primaryTextClass = 'text-red-600 dark:text-red-400'
        }
        let detailPreview = <span className='text-muted-foreground/40'>—</span>
        if (primary) {
          detailPreview = (
            <span
              className={cn(
                'truncate leading-snug group-hover:underline',
                primaryTextClass
              )}
            >
              {primary.text}
              {hasMore && (
                <span className='text-muted-foreground/40 ml-0.5'>
                  +{segments.length - 1}
                </span>
              )}
            </span>
          )
        } else if (log.content) {
          detailPreview = (
            <span className='text-muted-foreground truncate group-hover:underline'>
              {log.content}
            </span>
          )
        }

        return (
          <>
            <button
              type='button'
              className='group flex max-w-[200px] items-center gap-1 text-left text-xs'
              onClick={() => setDialogOpen(true)}
              title={t('Click to view full details')}
            >
              {detailPreview}
            </button>
            <DetailsDialog
              log={log}
              isAdmin={isAdmin}
              isRoot={isRoot}
              open={dialogOpen}
              onOpenChange={setDialogOpen}
            />
          </>
        )
      },
      size: 180,
      maxSize: 200,
    }
  )

  return columns
}
