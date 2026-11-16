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
import { useQuery } from '@tanstack/react-query'
import { Gauge, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ChannelAvatar } from '@/features/channel-status/components/channel-avatar'
import {
  formatDurationMs,
  formatSuccessRate,
} from '@/features/channels/lib/channel-status-display'
import { formatNumber, formatTimeStr } from '@/lib/format'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import {
  CHANNEL_SPEED_WINDOWS,
  CHANNEL_SPEED_WINDOW_SECONDS,
  getChannelSpeed,
  type ChannelSpeedItem,
  type ChannelSpeedWindow,
} from './api'

const AUTO_REFRESH_MS = 5_000
const LIVE_TRAFFIC_WINDOW_SECONDS = 15 * 60
const LIVE_TRAFFIC_REFRESH_MS = 60 * 1000
const QUERY_PREFIX = ['channel-speed'] as const

function formatSpeed(tps: number): string {
  if (!Number.isFinite(tps) || tps <= 0) return '—'
  if (tps >= 1000) return `${(tps / 1000).toFixed(1)}k`
  if (tps >= 10) return tps.toFixed(1)
  if (tps < 0.01) return '<0.01'
  return tps.toFixed(2)
}

function formatRequestRate(requests: number, seconds: number): string {
  if (!Number.isFinite(requests) || requests <= 0 || seconds <= 0) return '—'
  const rate = requests / seconds
  if (rate >= 1) return rate.toFixed(1)
  if (rate >= 0.01) return rate.toFixed(2)
  return '<0.01'
}

function channelState(item: ChannelSpeedItem): {
  label: string
  variant: 'success' | 'neutral'
} {
  if (!item.enabled) return { label: 'Paused', variant: 'neutral' }
  if (item.requests <= 0) return { label: 'Idle', variant: 'neutral' }
  return { label: 'Active', variant: 'success' }
}

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
        <span className='text-muted-foreground min-w-0 truncate text-[11px]'>
          {props.caption}
        </span>
      </div>
    </div>
  )
}

function ChannelSpeedCard(props: {
  item: ChannelSpeedItem
  maxSpeed: number
  outputSpeed: number
  windowSeconds: number
  /** 固定近 15 分钟总 token/s；与可切换窗口的整体输出速度不同口径。 */
  liveTps: number | null
  /** 输出速度用的窗口（5m/15m/1h），直接标在卡上避免和实时流量混淆。 */
  windowLabel: string
}) {
  const { t, i18n } = useTranslation()
  const state = channelState(props.item)
  const hasSpeed = props.outputSpeed > 0
  const width =
    hasSpeed && props.maxSpeed > 0
      ? Math.max(2, (props.outputSpeed / props.maxSpeed) * 100)
      : 0

  return (
    <article className='border-border/70 bg-card shadow-control flex min-w-0 flex-col gap-3 rounded-xl border p-4'>
      <header className='flex min-w-0 items-center gap-2'>
        <ChannelAvatar name={props.item.channel} />
        <div className='min-w-0 flex-1'>
          <div
            className='truncate text-sm font-medium'
            title={props.item.channel}
          >
            {props.item.channel}
          </div>
          <div className='text-muted-foreground text-[11px]'>
            {formatNumber(props.item.requests, i18n.language)} {t('Requests')}
          </div>
        </div>
        <StatusBadge
          label={t(state.label)}
          variant={state.variant}
          size='sm'
          copyable={false}
          className='shrink-0'
        />
      </header>

      <div
        className='flex items-end justify-between gap-2'
        title={t(
          'Output speed = all completion tokens produced by this channel in the window / window seconds.'
        )}
      >
        <div className='flex min-w-0 items-baseline gap-1'>
          <span
            className={cn(
              'font-mono text-2xl font-semibold tabular-nums',
              !hasSpeed && 'text-muted-foreground'
            )}
          >
            {hasSpeed ? formatSpeed(props.outputSpeed) : '—'}
          </span>
          <span className='text-muted-foreground text-xs'>tok/s</span>
        </div>
        <span className='text-muted-foreground flex flex-col items-end text-[11px] whitespace-nowrap'>
          <span>{t('Output speed')}</span>
          <span className='text-[10px]'>{props.windowLabel}</span>
        </span>
      </div>
      <div
        className='text-muted-foreground flex items-center justify-between gap-2 text-[11px]'
        title={t(
          'Generation speed = all completion tokens / generation time across every request in the window, so concurrent streams add up.'
        )}
      >
        <span>{t('Generation speed')}</span>
        <span className='font-mono tabular-nums'>
          {props.item.generation_tokens_per_second > 0
            ? `${formatSpeed(props.item.generation_tokens_per_second)} tok/s`
            : '—'}
        </span>
      </div>
      <div className='bg-muted/50 h-1.5 overflow-hidden rounded-full'>
        {hasSpeed && (
          <div
            className='bg-chart-1 h-full rounded-full transition-[width] duration-500'
            style={{ width: `${width}%` }}
          />
        )}
      </div>

      <dl className='grid grid-cols-2 gap-x-3 gap-y-1.5 text-xs'>
        <div className='flex min-w-0 items-center justify-between gap-2'>
          <dt className='text-muted-foreground truncate'>{t('Requests/s')}</dt>
          <dd className='font-mono tabular-nums'>
            {formatRequestRate(props.item.requests, props.windowSeconds)}
          </dd>
        </div>
        <div className='flex min-w-0 items-center justify-between gap-2'>
          <dt className='text-muted-foreground truncate'>
            {t('Success rate')}
          </dt>
          <dd className='font-mono tabular-nums'>
            {formatSuccessRate(props.item.success_rate)}
          </dd>
        </div>
        <div className='flex min-w-0 items-center justify-between gap-2'>
          <dt className='text-muted-foreground truncate'>{t('Latency')}</dt>
          <dd className='font-mono tabular-nums'>
            {formatDurationMs(props.item.p50_total_ms)}
          </dd>
        </div>
        <div className='flex min-w-0 items-center justify-between gap-2'>
          <dt className='text-muted-foreground truncate'>
            {t('Output tokens')}
          </dt>
          <dd className='font-mono tabular-nums'>
            {formatNumber(props.item.tokens_out, i18n.language)}
          </dd>
        </div>
        <div
          className='border-border/50 col-span-2 flex min-w-0 items-center justify-between gap-2 border-t pt-1.5'
          title={t(
            'Total tokens (input + output) processed over the last 15 minutes, including retries.'
          )}
        >
          <dt className='text-muted-foreground truncate'>
            {t('Live traffic')} · {t('Fixed 15m')}
          </dt>
          <dd className='font-mono tabular-nums'>
            {props.liveTps != null && props.liveTps > 0
              ? `${formatSpeed(props.liveTps)} tok/s`
              : '—'}
          </dd>
        </div>
      </dl>
    </article>
  )
}

function IdleChannelChips(props: { items: ChannelSpeedItem[] }) {
  const { t } = useTranslation()
  if (props.items.length === 0) return null
  return (
    <div className='border-border/70 overflow-hidden rounded-lg border'>
      <div className='bg-muted/25 flex items-center justify-between gap-2 px-3 py-2'>
        <span className='text-xs font-medium'>{t('Idle channels')}</span>
        <span className='text-muted-foreground text-[11px]'>
          {props.items.length}
        </span>
      </div>
      <div className='flex flex-wrap gap-1.5 p-3'>
        {props.items.map((item) => {
          const state = channelState(item)
          return (
            <span
              key={item.channel}
              className='bg-background inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs'
            >
              <span
                className='max-w-48 truncate font-mono'
                title={item.channel}
              >
                {item.channel}
              </span>
              <StatusBadge
                label={t(state.label)}
                variant={state.variant}
                size='sm'
                copyable={false}
              />
              <span className='text-muted-foreground font-mono'>—</span>
            </span>
          )
        })}
      </div>
    </div>
  )
}

function SpeedSkeleton() {
  return (
    <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
      {Array.from({ length: 3 }, (_, index) => (
        <div
          key={`channel-speed-skeleton-${index}`}
          className='border-border/70 bg-card rounded-xl border p-4'
        >
          <Skeleton className='h-4 w-32' />
          <Skeleton className='mt-4 h-8 w-24' />
          <Skeleton className='mt-3 h-1.5 w-full' />
          <Skeleton className='mt-4 h-12 w-full' />
        </div>
      ))}
    </div>
  )
}

/**
 * 实时路由页内的「渠道速度」面板（ui-spec §6 实时路由）。
 *
 * 短窗（5m/15m/1h）按渠道显示输出速度与请求速率，5s 自动刷新、页面隐藏暂停。
 * 有流量的渠道用速度卡；空闲/停用渠道收进紧凑条带，避免整页被 "No traffic"
 * 大卡淹没。数据源 `/api/channel-speed`（api-spec §6.14），口径与模型状态一致。
 */
export function ChannelSpeedPanel(props: {
  window: ChannelSpeedWindow
  onWindowChange: (window: ChannelSpeedWindow) => void
}) {
  const { t, i18n } = useTranslation()
  const windowSeconds = CHANNEL_SPEED_WINDOW_SECONDS[props.window]
  const query = useQuery({
    queryKey: [...QUERY_PREFIX, props.window],
    queryFn: () => {
      const to = Math.floor(Date.now() / 1000)
      return getChannelSpeed({ from: to - windowSeconds, to })
    },
    staleTime: AUTO_REFRESH_MS - 1_000,
    refetchInterval: AUTO_REFRESH_MS,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: false,
    meta: { errorToast: false },
  })

  // 实时流量固定近 15 分钟，不随输出速度的 5m/15m/1h 窗口变化；15m 档位直接复用
  // 主查询，其他档位再取一次固定窗口，避免两次 5s 轮询。
  const liveQuery = useQuery({
    queryKey: [...QUERY_PREFIX, 'live-traffic', '15m'],
    queryFn: () => {
      const to = Math.floor(Date.now() / 1000)
      return getChannelSpeed({
        from: to - LIVE_TRAFFIC_WINDOW_SECONDS,
        to,
      })
    },
    enabled: props.window !== '15m',
    staleTime: LIVE_TRAFFIC_REFRESH_MS / 2,
    refetchInterval: LIVE_TRAFFIC_REFRESH_MS,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: false,
    meta: { errorToast: false },
  })

  const items = query.data?.items ?? []
  const liveItems =
    props.window === '15m' ? items : (liveQuery.data?.items ?? [])
  const liveByChannel = new Map(
    liveItems.map((item) => [
      item.channel,
      item.tokens_total / LIVE_TRAFFIC_WINDOW_SECONDS,
    ])
  )
  const totalLiveTps =
    liveItems.length > 0
      ? liveItems.reduce((sum, item) => sum + item.tokens_total, 0) /
        LIVE_TRAFFIC_WINDOW_SECONDS
      : null
  const activeItems = items
    .filter((item) => item.requests > 0)
    .sort(
      (a, b) =>
        b.output_tokens_per_second - a.output_tokens_per_second ||
        b.requests - a.requests ||
        a.channel.localeCompare(b.channel)
    )
  const idleItems = items.filter((item) => item.requests <= 0)
  const totalRequests = items.reduce((sum, item) => sum + item.requests, 0)
  const totalSuccesses = items.reduce((sum, item) => sum + item.successes, 0)
  // 渠道整体输出速度：窗口内全部 completion_tokens / 窗口秒数。
  const totalOutputSpeed = items.reduce(
    (sum, item) => sum + item.output_tokens_per_second,
    0
  )
  const activeChannels = items.filter(
    (item) => item.enabled && item.requests > 0
  ).length
  const maxSpeed = activeItems.reduce(
    (max, item) => Math.max(max, item.output_tokens_per_second),
    0
  )
  const successRate = totalRequests > 0 ? totalSuccesses / totalRequests : null
  const isFetching =
    query.isFetching || (props.window !== '15m' && liveQuery.isFetching)

  return (
    <section
      aria-label={t('Channel speed')}
      className='border-border/70 bg-card shadow-control overflow-hidden rounded-xl border'
    >
      <header className='border-border/60 flex flex-col gap-2 border-b px-4 py-3'>
        <div className='flex flex-wrap items-center gap-2'>
          <Gauge className='text-primary size-4' aria-hidden='true' />
          <h2 className='text-sm font-semibold'>{t('Channel speed')}</h2>
          <span className='text-muted-foreground hidden text-[11px] sm:inline'>
            {t('Output speed by channel, refreshed every 5 seconds.')}
          </span>
          <span className='ml-auto' />
          <Tabs
            value={props.window}
            onValueChange={(value) => {
              if (CHANNEL_SPEED_WINDOWS.includes(value as ChannelSpeedWindow)) {
                props.onWindowChange(value as ChannelSpeedWindow)
              }
            }}
          >
            <TabsList>
              {CHANNEL_SPEED_WINDOWS.map((value) => (
                <TabsTrigger key={value} value={value}>
                  {value}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <span className='text-muted-foreground inline-flex items-center gap-1 text-[11px]'>
            <span className='bg-success size-1.5 rounded-full motion-safe:animate-pulse' />
            {t('Live')} · 5s
          </span>
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={isFetching}
            onClick={() => {
              void query.refetch()
              if (props.window !== '15m') void liveQuery.refetch()
            }}
          >
            <RefreshCw
              className={cn('size-3.5', isFetching && 'animate-spin')}
            />
            {t('Refresh')}
          </Button>
        </div>
      </header>

      <div className='px-4 py-4'>
        {query.isPending && <SpeedSkeleton />}

        {query.isError && (
          <ErrorState
            title={t('Channel speed is unavailable')}
            description={
              getServerErrorMessage(query.error) ||
              t('Failed to load channel speed, please retry.')
            }
            onRetry={() => void query.refetch()}
            className='min-h-[180px]'
          />
        )}

        {query.isSuccess && items.length === 0 && (
          <EmptyState
            title={t('No channels yet')}
            description={t(
              'Add a channel and send a request to see live output speed.'
            )}
          />
        )}

        {query.isSuccess && items.length > 0 && (
          <div className='flex flex-col gap-3'>
            <div className='border-border/70 grid grid-cols-2 gap-x-6 gap-y-3 border-y py-3 lg:grid-cols-5'>
              <SummaryTile
                label={t('Channels')}
                value={`${activeChannels} / ${items.length}`}
                caption={t('Active')}
              />
              <SummaryTile
                label={t('Output speed')}
                value={formatSpeed(totalOutputSpeed)}
                caption='tok/s'
              />
              <SummaryTile
                label={t('Live traffic')}
                value={totalLiveTps == null ? '—' : formatSpeed(totalLiveTps)}
                caption='tok/s · 15m'
              />
              <SummaryTile
                label={t('Requests')}
                value={formatNumber(totalRequests, i18n.language)}
                caption={props.window}
              />
              <SummaryTile
                label={t('Success rate')}
                value={
                  successRate == null ? '—' : formatSuccessRate(successRate)
                }
                caption={t('Window')}
              />
            </div>

            {totalRequests === 0 && (
              <div className='border-border/70 bg-muted/25 flex items-center gap-2 rounded-lg border px-3 py-2 text-xs'>
                <Gauge
                  className='text-muted-foreground size-4'
                  aria-hidden='true'
                />
                <span>{t('No channel traffic in this window.')}</span>
              </div>
            )}

            {activeItems.length > 0 && (
              <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
                {activeItems.map((item) => (
                  <ChannelSpeedCard
                    key={item.channel}
                    item={item}
                    maxSpeed={maxSpeed}
                    outputSpeed={item.output_tokens_per_second}
                    windowSeconds={windowSeconds}
                    liveTps={liveByChannel.get(item.channel) ?? null}
                    windowLabel={props.window}
                  />
                ))}
              </div>
            )}

            <IdleChannelChips items={idleItems} />

            <div className='text-muted-foreground flex flex-wrap items-center justify-between gap-2 text-[11px]'>
              <span>
                {t(
                  'Output speed = all completion tokens in the window; live traffic = input + output tokens over the last 15 minutes.'
                )}
              </span>
              {query.dataUpdatedAt > 0 && (
                <span>
                  {t('Last updated')}{' '}
                  {formatTimeStr(new Date(query.dataUpdatedAt))}
                </span>
              )}
            </div>
          </div>
        )}
      </div>
    </section>
  )
}
