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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { formatCostYuan } from '@/features/keys/lib/cost'
import { formatTokens } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { PBRResolvedTimeRange } from '../../lib/time-range'
import { getPBRStats } from '../../pbr-stats-api'
import { PanelWrapper } from '../ui/panel-wrapper'

const STALE_TIME_MS = 60 * 1000
const HEATMAP_DAYS = 371 // 53 weeks
const HEATMAP_WEEKS = 53
const CLIENT_ROWS = 6

type HeatmapMetric = 'requests' | 'tokens' | 'cost'

interface ClientRow {
  group: string
  name: string
  requests: number
  tokens: number
  cost: number
}

function dayKey(date: Date): string {
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

function heatLevel(value: number, max: number): string {
  // 无数据当天保持近空白的格线，不用灰色块冒充「低使用量」（ui-spec §6 用量日历）。
  if (value <= 0 || max <= 0) return 'bg-foreground/[0.035]'
  const ratio = value / max
  if (ratio >= 0.75) return 'bg-emerald-600'
  if (ratio >= 0.5) return 'bg-emerald-500/80'
  if (ratio >= 0.25) return 'bg-emerald-500/55'
  return 'bg-emerald-500/30'
}

const HEATMAP_MONTHS = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
]

function heatValue(
  bucket: {
    requests?: number
    prompt_tokens?: number
    completion_tokens?: number
    estimated_cost?: number
  },
  metric: HeatmapMetric
): number {
  if (metric === 'requests') return bucket.requests ?? 0
  if (metric === 'tokens') {
    return (bucket.prompt_tokens ?? 0) + (bucket.completion_tokens ?? 0)
  }
  return bucket.estimated_cost ?? 0
}

function metricLabel(
  metric: HeatmapMetric,
  t: (key: string) => string
): string {
  if (metric === 'requests') return t('Requests')
  if (metric === 'tokens') return t('Tokens')
  return t('Cost')
}

/**
 * Overview usage insights (ui-spec §6 概览, usage-spec §6.3): cache hit rate,
 * client distribution and a 53-week calendar. It reads the hourly aggregate
 * table, so pruning detail logs does not erase history.
 */
export function UsageInsightsCard(props: { range: PBRResolvedTimeRange }) {
  const { t } = useTranslation()
  const [metric, setMetric] = useState<HeatmapMetric>('requests')
  const from = props.range.from
  const to = props.range.to

  const windowQuery = useQuery({
    queryKey: ['pbr-usage-insights', from, to],
    queryFn: () =>
      getPBRStats({
        granularity: props.range.granularity,
        from,
        to,
        groupBy: 'lane',
      }),
    staleTime: STALE_TIME_MS,
  })
  const clientQuery = useQuery({
    queryKey: ['pbr-usage-clients', from, to],
    queryFn: () =>
      getPBRStats({ granularity: 'day', from, to, groupBy: 'client' }),
    staleTime: STALE_TIME_MS,
  })
  const now = props.range.to
  const heatFrom = now - HEATMAP_DAYS * 86400
  const heatQuery = useQuery({
    queryKey: ['pbr-usage-heatmap', now],
    queryFn: () =>
      getPBRStats({
        granularity: 'day',
        from: heatFrom,
        to: now,
        groupBy: 'lane',
      }),
    staleTime: STALE_TIME_MS,
  })

  const cache = useMemo(() => {
    let input = 0
    let cacheRead = 0
    let cacheWrite = 0
    let reasoning = 0
    for (const bucket of windowQuery.data?.items ?? []) {
      input += bucket.input_tokens ?? 0
      cacheRead += bucket.cache_read_tokens ?? 0
      cacheWrite += bucket.cache_write_tokens ?? 0
      reasoning += bucket.reasoning_tokens ?? 0
    }
    const rate = input > 0 ? cacheRead / input : null
    return { input, cacheRead, cacheWrite, reasoning, rate }
  }, [windowQuery.data])

  const clients = useMemo<ClientRow[]>(() => {
    const byGroup = new Map<string, ClientRow>()
    for (const bucket of clientQuery.data?.items ?? []) {
      const group = bucket.client_group || bucket.group
      if (!group) continue
      const current = byGroup.get(group) ?? {
        group,
        name:
          group === 'script'
            ? t('Scripts')
            : bucket.client_name || bucket.group,
        requests: 0,
        tokens: 0,
        cost: 0,
      }
      current.requests += bucket.requests ?? 0
      current.tokens +=
        (bucket.prompt_tokens ?? 0) + (bucket.completion_tokens ?? 0)
      current.cost += bucket.estimated_cost ?? 0
      byGroup.set(group, current)
    }
    return [...byGroup.values()]
      .sort((a, b) => b.requests - a.requests)
      .slice(0, CLIENT_ROWS)
  }, [clientQuery.data, t])

  const heat = useMemo(() => {
    const days = new Map<string, number>()
    for (const bucket of heatQuery.data?.items ?? []) {
      const key = dayKey(new Date(bucket.bucket_ts * 1000))
      const value = heatValue(bucket, metric)
      days.set(key, (days.get(key) ?? 0) + value)
    }
    const today = new Date()
    today.setHours(0, 0, 0, 0)
    const start = new Date(today)
    start.setDate(start.getDate() - (HEATMAP_DAYS - 1))
    start.setDate(start.getDate() - start.getDay())
    const max = Math.max(0, ...days.values())
    // 按列组织（一周一列、周一到周日七行），月标签落在跨月的第一列上；
    // 这样网格不会因为 grid-rows 与 grid-flow-col 的组合被撑成单行。
    const weeks: { key: string; date: Date; value: number }[][] = []
    const monthLabels: { column: number; label: string }[] = []
    let lastMonth = -1
    for (let week = 0; week < HEATMAP_WEEKS; week++) {
      const column: { key: string; date: Date; value: number }[] = []
      for (let day = 0; day < 7; day++) {
        const date = new Date(start)
        date.setDate(start.getDate() + week * 7 + day)
        const key = dayKey(date)
        column.push({ key, date, value: days.get(key) ?? 0 })
      }
      const month = column[0].date.getMonth()
      if (month !== lastMonth) {
        monthLabels.push({ column: week, label: HEATMAP_MONTHS[month] })
        lastMonth = month
      }
      weeks.push(column)
    }
    return { weeks, monthLabels, max }
  }, [heatQuery.data, metric])

  const loading =
    windowQuery.isLoading || clientQuery.isLoading || heatQuery.isLoading
  const empty =
    (windowQuery.data?.items?.length ?? 0) === 0 &&
    (clientQuery.data?.items?.length ?? 0) === 0 &&
    (heatQuery.data?.items?.length ?? 0) === 0
  const failed = windowQuery.isError || clientQuery.isError || heatQuery.isError

  return (
    <PanelWrapper
      title={t('Usage insights')}
      description={t(
        'Cache hits, clients and the last 53 weeks, from the aggregate table.'
      )}
      loading={loading}
      empty={empty && !failed}
      emptyMessage={t('No usage recorded in this window yet')}
      height='h-72'
      contentClassName='space-y-4'
    >
      {failed ? (
        <div className='flex h-64 flex-col items-center justify-center gap-2 text-center'>
          <p className='text-muted-foreground text-xs'>
            {t('Usage insights are unavailable')}
          </p>
          <button
            type='button'
            className='text-primary text-xs underline-offset-4 hover:underline'
            onClick={() => {
              void windowQuery.refetch()
              void clientQuery.refetch()
              void heatQuery.refetch()
            }}
          >
            {t('Retry')}
          </button>
        </div>
      ) : (
        <>
          <div className='grid gap-3 lg:grid-cols-[minmax(0,18rem)_minmax(0,1fr)]'>
            <div className='bg-muted/20 rounded-md border p-3'>
              <div className='text-muted-foreground text-xs'>
                {t('Cache hit rate')}
              </div>
              <div className='mt-1 font-mono text-2xl font-semibold tabular-nums'>
                {cache.rate == null ? '—' : `${Math.round(cache.rate * 100)}%`}
              </div>
              <div className='text-muted-foreground mt-1 text-xs'>
                {cache.rate == null
                  ? t('No input tokens in this window')
                  : `${t('Cache read')} ${formatTokens(cache.cacheRead)} · ${t('Input')} ${formatTokens(cache.input)}`}
              </div>
              <div className='text-muted-foreground mt-1 text-xs'>
                {t('Cache write')} {formatTokens(cache.cacheWrite)} ·{' '}
                {t('Reasoning')} {formatTokens(cache.reasoning)}
              </div>
            </div>

            <div className='bg-muted/20 rounded-md border p-3'>
              <div className='mb-2 flex items-center justify-between gap-2'>
                <span className='text-xs font-semibold'>{t('Clients')}</span>
                <span className='text-muted-foreground text-xs'>
                  {t('By requests')}
                </span>
              </div>
              {clients.length === 0 ? (
                <p className='text-muted-foreground text-xs'>
                  {t('No identifiable clients in this window')}
                </p>
              ) : (
                <ul className='space-y-1.5'>
                  {clients.map((client) => {
                    const max = clients[0]?.requests || 1
                    return (
                      <li
                        key={client.group}
                        className='grid grid-cols-[minmax(0,8rem)_minmax(0,1fr)_4rem] items-center gap-2 text-xs'
                      >
                        <span className='truncate' title={client.group}>
                          {client.name}
                        </span>
                        <span className='bg-muted h-1.5 overflow-hidden rounded-full'>
                          <span
                            className='block h-full rounded-full bg-sky-500'
                            style={{
                              width: `${Math.max(2, (client.requests / max) * 100)}%`,
                            }}
                          />
                        </span>
                        <span className='text-muted-foreground text-right font-mono tabular-nums'>
                          {client.requests}
                        </span>
                      </li>
                    )
                  })}
                </ul>
              )}
            </div>
          </div>

          <div>
            <div className='mb-2 flex flex-wrap items-center justify-between gap-2'>
              <span className='text-xs font-semibold'>
                {t('Usage calendar')}
              </span>
              <div className='flex items-center gap-1'>
                {(['requests', 'tokens', 'cost'] as HeatmapMetric[]).map(
                  (option) => (
                    <button
                      key={option}
                      type='button'
                      onClick={() => setMetric(option)}
                      className={cn(
                        'rounded px-1.5 py-0.5 text-xs transition-colors',
                        metric === option
                          ? 'bg-muted text-foreground'
                          : 'text-muted-foreground hover:text-foreground'
                      )}
                    >
                      {metricLabel(option, t)}
                    </button>
                  )
                )}
                <span className='text-muted-foreground ml-2 text-xs'>
                  {t('Last 53 weeks')}
                </span>
              </div>
            </div>
            <div className='overflow-x-auto pb-1'>
              <div className='w-max'>
                <div
                  className='text-muted-foreground relative mb-1 h-3 text-[10px]'
                  aria-hidden='true'
                >
                  {heat.monthLabels.map((m) => (
                    <span
                      key={`${m.column}-${m.label}`}
                      className='absolute'
                      style={{ left: `${m.column * 14}px` }}
                    >
                      {m.label}
                    </span>
                  ))}
                </div>
                <div
                  className='flex gap-1'
                  role='img'
                  aria-label={t('Daily usage over the last 53 weeks')}
                >
                  {heat.weeks.map((week) => (
                    <div key={week[0].key} className='flex flex-col gap-1'>
                      {week.map((cell) => (
                        <span
                          key={cell.key}
                          className={cn(
                            'size-2.5 rounded-[2px]',
                            heatLevel(cell.value, heat.max)
                          )}
                          title={`${cell.key}: ${
                            metric === 'cost'
                              ? formatCostYuan(cell.value)
                              : formatTokens(cell.value)
                          }`}
                        />
                      ))}
                    </div>
                  ))}
                </div>
              </div>
            </div>
            <div className='text-muted-foreground mt-2 flex items-center justify-end gap-1 text-[10px]'>
              <span>{t('Less')}</span>
              {[
                'bg-foreground/[0.035]',
                'bg-emerald-500/30',
                'bg-emerald-500/55',
                'bg-emerald-500/80',
                'bg-emerald-600',
              ].map((tone) => (
                <span
                  key={tone}
                  className={cn('size-2.5 rounded-[2px]', tone)}
                />
              ))}
              <span>{t('More')}</span>
            </div>
          </div>
        </>
      )}
    </PanelWrapper>
  )
}
