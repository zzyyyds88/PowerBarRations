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
/*
渠道状态（Connections → Channel status）。

旧版是一张「渠道 × 上游模型」的平面卡片墙：渠道本身是否可用、哪些渠道没有
接入路由、谁在拖后腿，都要靠逐张卡片自己拼。这里改成监控页应有的层级：

1. 顶部汇总：渠道数、可用目标、窗口请求、加权成功率；
2. 渠道表：一行一个渠道，先回答"这个渠道现在健不健康"；
3. 展开行：复用渠道抽屉的「模型状态」表，回答"哪个上游模型拖后腿"；
4. 移动端：同一信息结构切卡片，指标三列排布，不横向滚动。

状态来自 `GET /api/channels/status` 的运行态（enabled / degraded /
available_count），成功率与延迟来自按渠道并发的 `GET /api/channels/{name}/status`。
没有请求的模型显示 `-`，不拿 0% / 0ms 冒充故障。
*/
import { useQueries, useQuery } from '@tanstack/react-query'
import { ChevronDown, RefreshCw } from 'lucide-react'
import { Fragment, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { ConsolePage } from '@/components/layout'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ChannelStatusModelsTable } from '@/features/channels/components/drawers/channel-status-models'
import { channelsQueryKeys } from '@/features/channels/lib'
import {
  formatDurationMs,
  formatModelSuccessRate,
  formatOutputTps,
  formatSuccessRate,
  resolveProtocolLabelKey,
} from '@/features/channels/lib/channel-status-display'
import {
  getChannelStatus,
  listChannelStatus,
  type ChannelStatusDetail,
  type ChannelStatusModelStat,
  type ChannelStatusSummaryItem,
  type ChannelStatusWindow,
} from '@/features/channels/status-api'
import { formatCostYuan } from '@/features/keys/lib/cost'
import { useMediaQuery } from '@/hooks/use-media-query'
import {
  formatCompactNumber,
  formatNumber,
  formatTokenAmount,
} from '@/lib/format'
import { getLobeIcon } from '@/lib/lobe-icon'
import { resolveModelProvider } from '@/lib/model-provider'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { ChannelAvatar } from './components/channel-avatar'

type ChannelHealth =
  | 'serving'
  | 'degraded'
  | 'unavailable'
  | 'paused'
  | 'unrouted'

const CHANNEL_HEALTH: Record<
  ChannelHealth,
  { labelKey: string; variant: StatusVariant }
> = {
  serving: { labelKey: 'Serving', variant: 'success' },
  degraded: { labelKey: 'Degraded', variant: 'warning' },
  unavailable: { labelKey: 'Unavailable', variant: 'danger' },
  paused: { labelKey: 'Paused', variant: 'neutral' },
  unrouted: { labelKey: 'Unrouted', variant: 'neutral' },
}

/** 监控页先看问题：不可用 > 降级 > 服务中 > 已暂停 > 未接入。 */
const CHANNEL_HEALTH_ORDER: Record<ChannelHealth, number> = {
  unavailable: 0,
  degraded: 1,
  serving: 2,
  paused: 3,
  unrouted: 4,
}

const SKELETON_KEYS = Array.from(
  { length: 6 },
  (_, index) => `channel-status-skeleton-${index}`
)

interface ChannelStatusRow {
  item: ChannelStatusSummaryItem
  detail: ChannelStatusDetail | undefined
  detailPending: boolean
  detailError: boolean
  retryDetail: () => void
}

/**
 * 渠道级健康状态：配置态（enabled）优先，其次看运行态（成员可用数），
 * 最后用窗口成功率兜底。历史成功率只在有足够样本时参与判断，避免
 * 一两条失败就把新渠道标黄。
 */
function resolveChannelHealth(item: ChannelStatusSummaryItem): ChannelHealth {
  if (!item.enabled) return 'paused'
  if (item.member_count <= 0) return 'unrouted'
  if (item.available_count <= 0) return 'unavailable'
  if (item.degraded || item.available_count < item.member_count) {
    return 'degraded'
  }
  if (item.requests >= 20 && item.success_rate < 0.95) return 'degraded'
  return 'serving'
}

/** 成功率文字色：99% 以上绿、95% 以上琥珀、其余红；无样本保持静默。 */
function successTone(requests: number, rate: number): string {
  if (requests <= 0) return 'text-muted-foreground'
  if (rate >= 0.99) return 'text-[var(--tone-success-text)]'
  if (rate >= 0.95) return 'text-[var(--tone-warning-text)]'
  return 'text-[var(--tone-destructive-text)]'
}

/** 两个分位都没有样本时只显示一个破折号，避免 `- / -` 的机械感。 */
function formatLatencyRange(detail: ChannelStatusDetail | undefined): string {
  if (!detail) return '-'
  const p50 = formatDurationMs(detail.summary.p50_total_ms)
  const p95 = formatDurationMs(detail.summary.p95_total_ms)
  if (p50 === '-' && p95 === '-') return '-'
  return `${p50} / ${p95}`
}

/**
 * 扁平指标条：保留原来的四个口径（渠道/目标/请求/成功率），但不再用四张
 * 大色块卡片把首屏撑满。数字与说明同排，移动端两列。
 */
function SummaryTile(props: { label: string; value: string; caption: string }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground truncate text-[11px] font-medium tracking-wide uppercase'>
        {props.label}
      </div>
      <div className='mt-0.5 flex items-baseline gap-1.5'>
        <span className='truncate font-mono text-lg font-semibold tabular-nums'>
          {props.value}
        </span>
        <span
          className='text-muted-foreground min-w-0 truncate text-[11px]'
          title={props.caption}
        >
          {props.caption}
        </span>
      </div>
    </div>
  )
}

function SummaryStrip(props: {
  items: ChannelStatusSummaryItem[]
  overallRate: number | null
}) {
  const { t, i18n } = useTranslation()
  const enabled = props.items.filter((item) => item.enabled).length
  const members = props.items.reduce((sum, item) => sum + item.member_count, 0)
  const available = props.items.reduce(
    (sum, item) => sum + item.available_count,
    0
  )
  const requests = props.items.reduce((sum, item) => sum + item.requests, 0)
  const failures = props.items.reduce((sum, item) => sum + item.failures, 0)
  const serving = props.items.filter(
    (item) => resolveChannelHealth(item) === 'serving'
  ).length

  return (
    <div className='border-border/70 grid grid-cols-2 gap-x-6 gap-y-3 border-y py-3 lg:grid-cols-4'>
      <SummaryTile
        label={t('Channels')}
        value={formatNumber(props.items.length, i18n.language)}
        caption={`${enabled} ${t('Enabled')} · ${props.items.length - enabled} ${t('Paused')}`}
      />
      <SummaryTile
        label={t('Targets')}
        value={`${formatNumber(available, i18n.language)} / ${formatNumber(members, i18n.language)}`}
        caption={`${t('Available')} / ${t('Total')}`}
      />
      <SummaryTile
        label={t('Requests')}
        value={formatCompactNumber(requests, i18n.language)}
        caption={`${formatNumber(failures, i18n.language)} ${t('Failures')}`}
      />
      <SummaryTile
        label={t('Success rate')}
        value={formatSuccessRate(props.overallRate)}
        caption={`${serving} ${t('Serving')}`}
      />
    </div>
  )
}

function ChannelStatusSkeleton() {
  return (
    <div className='flex flex-col gap-3'>
      <div className='grid grid-cols-2 gap-2 lg:grid-cols-4'>
        {SKELETON_KEYS.slice(0, 4).map((key) => (
          <div
            key={key}
            className='border-border/70 bg-muted/40 rounded-control border px-3 py-2'
          >
            <Skeleton className='h-3 w-16' />
            <Skeleton className='mt-2 h-5 w-24' />
            <Skeleton className='mt-2 h-2.5 w-20' />
          </div>
        ))}
      </div>
      <div className='border-border bg-card rounded-lg border'>
        {SKELETON_KEYS.map((key) => (
          <div
            key={key}
            className='border-border/60 flex items-center gap-3 border-b px-3 py-3 last:border-b-0'
          >
            <Skeleton className='size-8 rounded-lg' />
            <div className='flex-1 space-y-1.5'>
              <Skeleton className='h-3.5 w-40 max-w-[40%]' />
              <Skeleton className='h-2.5 w-24' />
            </div>
            <Skeleton className='h-5 w-16' />
          </div>
        ))}
      </div>
    </div>
  )
}

function ChannelModelList(props: { models: ChannelStatusModelStat[] }) {
  const { t, i18n } = useTranslation()

  if (props.models.length === 0) {
    return (
      <p className='text-muted-foreground text-xs'>
        {t('No model data in this window')}
      </p>
    )
  }

  return (
    <ul className='divide-border/60 divide-y'>
      {props.models.map((model) => {
        const provider = resolveModelProvider(model.model)
        return (
          <li
            key={model.model}
            className='flex flex-col gap-1 py-2 first:pt-0 last:pb-0'
          >
            <div className='flex items-center gap-2'>
              {provider?.icon ? (
                getLobeIcon(provider.icon, 20)
              ) : (
                <span className='bg-muted text-muted-foreground flex size-5 shrink-0 items-center justify-center rounded-full text-[10px] font-medium'>
                  {model.model.slice(0, 1).toUpperCase()}
                </span>
              )}
              <span
                className='min-w-0 flex-1 truncate font-mono text-xs'
                title={model.model}
              >
                {model.model}
              </span>
              <span
                className={cn(
                  'shrink-0 text-xs font-medium tabular-nums',
                  successTone(model.requests, model.success_rate)
                )}
              >
                {formatModelSuccessRate(model)}
              </span>
            </div>
            <div className='text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 pl-7 text-[11px] lowercase tabular-nums'>
              <span>
                {formatNumber(model.requests, i18n.language)} {t('Requests')}
              </span>
              <span>p50 {formatDurationMs(model.p50_total_ms)}</span>
              <span>{formatOutputTps(model.output_tps)} tok/s</span>
              <span>
                {formatTokenAmount(model.tokens_out, i18n.language)}{' '}
                {t('Output tokens')}
              </span>
            </div>
          </li>
        )
      })}
    </ul>
  )
}

function ChannelModelsPanel(props: { row: ChannelStatusRow; mobile: boolean }) {
  const { t } = useTranslation()

  if (props.row.detailPending) {
    return <Skeleton className='h-24 w-full' />
  }

  if (props.row.detailError) {
    return (
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <span className='text-xs text-[var(--tone-destructive-text)]'>
          {t('Failed to load channel status, please retry.')}
        </span>
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={props.row.retryDetail}
        >
          {t('Retry')}
        </Button>
      </div>
    )
  }

  if (!props.row.detail) return null

  return (
    <div className='flex flex-col gap-2'>
      <div className='flex items-center justify-between gap-2'>
        <span className='text-xs font-medium'>{t('Model status')}</span>
        <span className='text-muted-foreground text-xs'>
          {props.row.detail.models.length}{' '}
          {t(props.row.detail.models.length === 1 ? 'Model' : 'Models')}
        </span>
      </div>
      {props.mobile ? (
        <ChannelModelList models={props.row.detail.models} />
      ) : (
        <ChannelStatusModelsTable models={props.row.detail.models} />
      )}
    </div>
  )
}

function ChannelTable(props: {
  rows: ChannelStatusRow[]
  expanded: Record<string, boolean>
  onToggle: (channel: string) => void
}) {
  const { t, i18n } = useTranslation()

  return (
    <div className='border-border bg-card shadow-control overflow-hidden rounded-lg border'>
      <Table>
        <TableHeader>
          <TableRow className='bg-muted/40 hover:bg-muted/40'>
            <TableHead className='min-w-[220px]'>{t('Channel')}</TableHead>
            <TableHead className='w-[110px]'>{t('Status')}</TableHead>
            <TableHead className='w-[90px] text-right'>
              {t('Targets')}
            </TableHead>
            <TableHead className='w-[100px] text-right'>
              {t('Requests')}
            </TableHead>
            <TableHead className='w-[110px] text-right'>
              {t('Success rate')}
            </TableHead>
            <TableHead className='w-[120px] text-right'>
              {t('Latency')}
              <span className='text-muted-foreground ml-1 text-[10px] font-normal'>
                p50 / p95
              </span>
            </TableHead>
            <TableHead className='w-[90px] text-right'>{t('Cost')}</TableHead>
            <TableHead className='w-9'>
              <span className='sr-only'>{t('Details')}</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.rows.map((row, index) => {
            const { item } = row
            const health = resolveChannelHealth(item)
            const presentation = CHANNEL_HEALTH[health]
            const isOpen = props.expanded[item.channel] ?? false
            const regionId = `channel-models-${index}`
            const protocolKey = resolveProtocolLabelKey(null, item.type)
            const protocol = protocolKey ? t(protocolKey) : item.type

            return (
              <Fragment key={item.channel}>
                <TableRow className={cn(isOpen && 'bg-muted/20')}>
                  <TableCell>
                    <button
                      type='button'
                      className='flex w-full min-w-0 items-center gap-2 text-left'
                      aria-expanded={isOpen}
                      aria-controls={isOpen ? regionId : undefined}
                      onClick={() => props.onToggle(item.channel)}
                    >
                      <ChannelAvatar name={item.channel} />
                      <span className='min-w-0'>
                        <span className='block truncate text-sm font-medium'>
                          {item.channel}
                        </span>
                        <span className='text-muted-foreground block truncate text-xs'>
                          {protocol} · {item.lane_count} {t('Routes')}
                        </span>
                      </span>
                    </button>
                  </TableCell>
                  <TableCell>
                    <StatusBadge
                      label={t(presentation.labelKey)}
                      variant={presentation.variant}
                      size='sm'
                      copyable={false}
                    />
                  </TableCell>
                  <TableCell className='text-right'>
                    <span
                      className={cn(
                        'tabular-nums',
                        item.member_count > 0 &&
                          item.available_count <= 0 &&
                          'text-[var(--tone-destructive-text)]',
                        item.available_count > 0 &&
                          item.available_count < item.member_count &&
                          'text-[var(--tone-warning-text)]'
                      )}
                    >
                      {item.member_count > 0
                        ? `${item.available_count}/${item.member_count}`
                        : '-'}
                    </span>
                  </TableCell>
                  <TableCell className='text-right tabular-nums'>
                    {formatNumber(item.requests, i18n.language)}
                  </TableCell>
                  <TableCell className='text-right'>
                    <span
                      className={cn(
                        'tabular-nums',
                        successTone(item.requests, item.success_rate)
                      )}
                    >
                      {item.requests > 0
                        ? formatSuccessRate(item.success_rate)
                        : '-'}
                    </span>
                  </TableCell>
                  <TableCell className='text-right whitespace-nowrap tabular-nums'>
                    {formatLatencyRange(row.detail)}
                  </TableCell>
                  <TableCell className='text-right whitespace-nowrap tabular-nums'>
                    {formatCostYuan(item.estimated_cost)}
                  </TableCell>
                  <TableCell className='text-right'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      aria-label={`${t(isOpen ? 'Collapse' : 'Expand')} ${item.channel}`}
                      aria-expanded={isOpen}
                      aria-controls={isOpen ? regionId : undefined}
                      onClick={() => props.onToggle(item.channel)}
                    >
                      <ChevronDown
                        className={cn(
                          'size-4 transition-transform',
                          isOpen && 'rotate-180'
                        )}
                      />
                    </Button>
                  </TableCell>
                </TableRow>
                {isOpen && (
                  <TableRow className='bg-muted/10 hover:bg-muted/10'>
                    <TableCell
                      id={regionId}
                      colSpan={8}
                      className='border-border/60 border-t p-0'
                    >
                      <div className='px-4 py-3'>
                        <ChannelModelsPanel row={row} mobile={false} />
                      </div>
                    </TableCell>
                  </TableRow>
                )}
              </Fragment>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}

function MobileMetric(props: {
  label: string
  value: string
  tone?: string
  className?: string
}) {
  return (
    <div className={cn('min-w-0', props.className)}>
      <div className='text-muted-foreground truncate text-[10px] uppercase'>
        {props.label}
      </div>
      <div
        className={cn(
          'mt-0.5 truncate text-xs font-medium tabular-nums',
          props.tone
        )}
      >
        {props.value}
      </div>
    </div>
  )
}

function ChannelMobileList(props: {
  rows: ChannelStatusRow[]
  expanded: Record<string, boolean>
  onToggle: (channel: string) => void
}) {
  const { t, i18n } = useTranslation()

  return (
    <div className='flex flex-col gap-2'>
      {props.rows.map((row, index) => {
        const { item } = row
        const health = resolveChannelHealth(item)
        const presentation = CHANNEL_HEALTH[health]
        const isOpen = props.expanded[item.channel] ?? false
        const regionId = `mobile-channel-models-${index}`
        const protocolKey = resolveProtocolLabelKey(null, item.type)
        const protocol = protocolKey ? t(protocolKey) : item.type

        return (
          <article
            key={item.channel}
            className='border-border bg-card shadow-control overflow-hidden rounded-lg border'
          >
            <button
              type='button'
              className='flex w-full min-w-0 items-center gap-2 px-3 py-2.5 text-left'
              aria-expanded={isOpen}
              aria-controls={isOpen ? regionId : undefined}
              onClick={() => props.onToggle(item.channel)}
            >
              <ChannelAvatar name={item.channel} />
              <span className='min-w-0 flex-1'>
                <span className='flex min-w-0 items-center gap-1.5'>
                  <span className='min-w-0 flex-1 truncate text-sm font-medium'>
                    {item.channel}
                  </span>
                  <StatusBadge
                    label={t(presentation.labelKey)}
                    variant={presentation.variant}
                    size='sm'
                    copyable={false}
                    className='shrink-0'
                  />
                </span>
                <span className='text-muted-foreground mt-0.5 block truncate text-xs'>
                  {protocol} · {item.lane_count} {t('Routes')}
                </span>
              </span>
              <ChevronDown
                className={cn(
                  'text-muted-foreground size-4 shrink-0 transition-transform',
                  isOpen && 'rotate-180'
                )}
              />
            </button>

            <div className='border-border/60 grid grid-cols-3 gap-2 border-t px-3 py-2'>
              <MobileMetric
                label={t('Requests')}
                value={formatNumber(item.requests, i18n.language)}
              />
              <MobileMetric
                label={t('Success rate')}
                value={
                  item.requests > 0 ? formatSuccessRate(item.success_rate) : '-'
                }
                tone={successTone(item.requests, item.success_rate)}
              />
              <MobileMetric
                label={t('Targets')}
                value={
                  item.member_count > 0
                    ? `${item.available_count}/${item.member_count}`
                    : '-'
                }
                tone={cn(
                  item.member_count > 0 &&
                    item.available_count <= 0 &&
                    'text-[var(--tone-destructive-text)]'
                )}
              />
              <MobileMetric
                label={t('Latency')}
                value={formatLatencyRange(row.detail)}
                className='col-span-2'
              />
              <MobileMetric
                label={t('Cost')}
                value={formatCostYuan(item.estimated_cost)}
              />
            </div>

            {isOpen && (
              <div
                id={regionId}
                className='border-border/60 border-t px-3 py-2.5'
              >
                <ChannelModelsPanel row={row} mobile />
              </div>
            )}
          </article>
        )
      })}
    </div>
  )
}

export function ChannelStatus(
  props: {
    window?: ChannelStatusWindow
    onWindowChange?: (window: ChannelStatusWindow) => void
  } = {}
) {
  const { t } = useTranslation()
  const isDesktop = useMediaQuery('(min-width: 768px)')
  const [localWindow, setLocalWindow] = useState<ChannelStatusWindow>('24h')
  const timeWindow = props.window ?? localWindow
  const setTimeWindow = (window: ChannelStatusWindow) => {
    setLocalWindow(window)
    props.onWindowChange?.(window)
  }
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})

  const summaryQuery = useQuery({
    queryKey: [...channelsQueryKeys.all, 'status-list', timeWindow],
    queryFn: () => listChannelStatus(timeWindow),
    meta: { errorToast: false },
  })

  const items = summaryQuery.data?.items ?? []
  const detailQueries = useQueries({
    queries: items.map((item) => ({
      queryKey: channelsQueryKeys.status(item.channel, timeWindow),
      queryFn: () => getChannelStatus(item.channel, timeWindow),
      staleTime: 30 * 1000,
      meta: { errorToast: false },
    })),
  })

  const rows: ChannelStatusRow[] = items.map((item, index) => {
    const query = detailQueries[index]
    return {
      item,
      detail: query?.data,
      detailPending: query?.isPending ?? false,
      detailError: query?.isError ?? false,
      retryDetail: () => {
        void query?.refetch()
      },
    }
  })
  rows.sort((a, b) => {
    const healthDelta =
      CHANNEL_HEALTH_ORDER[resolveChannelHealth(a.item)] -
      CHANNEL_HEALTH_ORDER[resolveChannelHealth(b.item)]
    if (healthDelta !== 0) return healthDelta
    return (
      b.item.requests - a.item.requests ||
      a.item.channel.localeCompare(b.item.channel)
    )
  })

  const totalRequests = items.reduce((sum, item) => sum + item.requests, 0)
  const totalFailures = items.reduce((sum, item) => sum + item.failures, 0)
  const overallRate =
    totalRequests > 0 ? (totalRequests - totalFailures) / totalRequests : null

  const isFetching =
    summaryQuery.isFetching || detailQueries.some((query) => query.isFetching)

  const toggle = (channel: string) => {
    setExpanded((current) => ({
      ...current,
      [channel]: !current[channel],
    }))
  }

  const refresh = () => {
    void summaryQuery.refetch()
    for (const query of detailQueries) void query.refetch()
  }

  const headerActions = (
    <div className='flex items-center gap-2'>
      <Button
        type='button'
        size='sm'
        variant='outline'
        disabled={isFetching}
        onClick={refresh}
      >
        <RefreshCw className={cn('size-3.5', isFetching && 'animate-spin')} />
        {t('Refresh')}
      </Button>
      <Tabs
        value={timeWindow}
        onValueChange={(value) => setTimeWindow(value === '7d' ? '7d' : '24h')}
      >
        <TabsList>
          <TabsTrigger value='24h'>24h</TabsTrigger>
          <TabsTrigger value='7d'>7d</TabsTrigger>
        </TabsList>
      </Tabs>
    </div>
  )

  return (
    <ConsolePage
      title={t('Channel status')}
      description={t(
        'Availability, success rate and latency for every upstream channel.'
      )}
      actions={headerActions}
    >
      {summaryQuery.isPending && <ChannelStatusSkeleton />}

      {summaryQuery.isError && (
        <ErrorState
          title={t('Channel status is unavailable')}
          description={
            getServerErrorMessage(summaryQuery.error) ||
            t('Failed to load channel status, please retry.')
          }
          onRetry={() => void summaryQuery.refetch()}
          className='min-h-[240px]'
        />
      )}

      {summaryQuery.data && items.length === 0 && (
        <EmptyState
          title={t('No channel status yet')}
          description={t(
            'Channel status appears after the first request is recorded.'
          )}
        />
      )}

      {items.length > 0 && (
        <div className='flex flex-col gap-3'>
          <SummaryStrip items={items} overallRate={overallRate} />

          <div className='mt-1 flex items-end justify-between gap-2'>
            <h2 className='text-sm font-semibold'>{t('Channel details')}</h2>
            <span className='text-muted-foreground text-xs'>
              {items.length} {t('Channels')}
            </span>
          </div>

          {isDesktop ? (
            <ChannelTable rows={rows} expanded={expanded} onToggle={toggle} />
          ) : (
            <ChannelMobileList
              rows={rows}
              expanded={expanded}
              onToggle={toggle}
            />
          )}
        </div>
      )}
    </ConsolePage>
  )
}
