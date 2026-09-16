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
import { type ReactNode, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import {
  getPBRStats,
  PBR_CHANNEL_MODEL_SEPARATOR,
  type PBRStatBucket,
  type PBRStatsGroupBy,
} from '../../pbr-stats-api'

// 模型/成本分析（PBR 口径）。请求数、token、成功率与上游花费全部来自
// GET /api/stats 的聚合；上游单价在渠道里配置（渠道价 > 全局默认 > 不折算）。

const RANGE_OPTIONS = [
  { value: '24h', label: '最近 24 小时', seconds: 24 * 3600, granularity: 'hour' },
  { value: '7d', label: '最近 7 天', seconds: 7 * 24 * 3600, granularity: 'day' },
  { value: '30d', label: '最近 30 天', seconds: 30 * 24 * 3600, granularity: 'day' },
] as const

const GROUP_OPTIONS: { value: PBRStatsGroupBy; label: string }[] = [
  { value: 'channel', label: '按渠道' },
  { value: 'model', label: '按模型' },
  { value: 'lane', label: '按车道' },
  { value: 'key', label: '按密钥' },
  { value: 'channel_model', label: '渠道 × 模型' },
]

const EMPTY_BUCKETS: PBRStatBucket[] = []

function formatCost(value: number): string {
  if (!Number.isFinite(value) || value === 0) return '¥0'
  if (Math.abs(value) < 0.01) return `¥${value.toFixed(4)}`
  return `¥${value.toFixed(2)}`
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(
    Number.isFinite(value) ? value : 0
  )
}

function formatBucket(ts: number, granularity: 'hour' | 'day'): string {
  const date = new Date(ts * 1000)
  if (granularity === 'day') {
    return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  }
  return date.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
  })
}

function statCard(label: string, value: string) {
  return (
    <div className='bg-card/60 rounded-lg border px-4 py-3'>
      <div className='text-muted-foreground text-xs'>{label}</div>
      <div className='mt-1 font-mono text-lg font-semibold tabular-nums'>
        {value}
      </div>
    </div>
  )
}

export function PbrAnalyticsDashboard(props: {
  defaultGroupBy?: PBRStatsGroupBy
  groupOptions?: PBRStatsGroupBy[]
}) {
  const { t } = useTranslation()
  const [rangeKey, setRangeKey] =
    useState<(typeof RANGE_OPTIONS)[number]['value']>('7d')
  const [groupBy, setGroupBy] = useState<PBRStatsGroupBy>(
    props.defaultGroupBy ?? 'channel'
  )
  const allowedGroups = props.groupOptions ?? [
    'channel',
    'model',
    'lane',
    'key',
    'channel_model',
  ]
  const visibleGroupOptions = GROUP_OPTIONS.filter((option) =>
    allowedGroups.includes(option.value)
  )
  const range =
    RANGE_OPTIONS.find((item) => item.value === rangeKey) ?? RANGE_OPTIONS[1]

  const query = useQuery({
    queryKey: ['pbr-analytics-stats', rangeKey, groupBy],
    queryFn: () => {
      // eslint-disable-next-line react/purity -- query functions run outside render
      const to = Math.floor(Date.now() / 1000)
      const from = to - range.seconds
      return getPBRStats({
        granularity: range.granularity,
        from,
        to,
        groupBy,
      })
    },
    refetchOnWindowFocus: false,
  })

  const items = query.data?.items ?? EMPTY_BUCKETS

  const totals = useMemo(
    () =>
      items.reduce(
        (acc, item) => {
          acc.cost += item.estimated_cost || 0
          acc.requests += item.requests || 0
          acc.successes += item.successes || 0
          acc.tokens +=
            (item.prompt_tokens || 0) + (item.completion_tokens || 0)
          return acc
        },
        { cost: 0, requests: 0, successes: 0, tokens: 0 }
      ),
    [items]
  )

  const timeline = useMemo(() => {
    const byBucket = new Map<number, number>()
    for (const item of items) {
      byBucket.set(
        item.bucket_ts,
        (byBucket.get(item.bucket_ts) ?? 0) + (item.estimated_cost || 0)
      )
    }
    return [...byBucket.entries()]
      .sort((a, b) => a[0] - b[0])
      .map(([ts, cost]) => ({ ts, cost }))
  }, [items])

  const maxBucketCost = timeline.reduce((max, item) => Math.max(max, item.cost), 0)

  const distribution = useMemo(() => {
    const byGroup = new Map<
      string,
      { cost: number; requests: number; tokens: number }
    >()
    for (const item of items) {
      const key = item.group || '(未记录)'
      const current = byGroup.get(key) ?? { cost: 0, requests: 0, tokens: 0 }
      current.cost += item.estimated_cost || 0
      current.requests += item.requests || 0
      current.tokens += (item.prompt_tokens || 0) + (item.completion_tokens || 0)
      byGroup.set(key, current)
    }
    return [...byGroup.entries()]
      .map(([key, value]) => ({ key, ...value }))
      .sort((a, b) => b.cost - a.cost)
  }, [items])

  // 「渠道 × 模型」模式把 group 拆成渠道与模型两列；distribution 已按成本倒序。
  const channelModelRows = useMemo(() => {
    if (groupBy !== 'channel_model') return []
    return distribution.map((row) => {
      const [channel, model] = row.key.split(PBR_CHANNEL_MODEL_SEPARATOR)
      return {
        key: row.key,
        channel: channel || '(未记录)',
        model: model ?? '',
        cost: row.cost,
        requests: row.requests,
        tokens: row.tokens,
      }
    })
  }, [distribution, groupBy])

  const successRate =
    totals.requests > 0 ? (totals.successes / totals.requests) * 100 : 0

  let body: ReactNode
  if (query.isLoading) {
    body = <LoadingState />
  } else if (query.isError) {
    body = (
      <ErrorState
        title={t('Failed to load statistics')}
        description={
          query.error instanceof Error ? query.error.message : undefined
        }
      />
    )
  } else if (items.length === 0) {
    body = (
      <EmptyState
        title={t('No cost data yet')}
        description={t(
          'Configure upstream unit prices in the channel to see cost accounting here.'
        )}
      />
    )
  } else {
    body = (
      <>
        <div className='bg-card/60 rounded-lg border p-4'>
          <div className='text-muted-foreground mb-3 text-xs'>
            {t('Cost over time')}
          </div>
          <div className='flex h-40 items-end gap-1'>
            {timeline.map((item) => (
              <div
                key={item.ts}
                className='group relative flex flex-1 flex-col items-center justify-end'
                title={`${formatBucket(item.ts, range.granularity)} · ${formatCost(item.cost)}`}
              >
                <div
                  className='bg-sky-500/70 w-full rounded-sm'
                  style={{
                    height:
                      maxBucketCost > 0
                        ? `${Math.max(2, (item.cost / maxBucketCost) * 100)}%`
                        : '2px',
                  }}
                />
              </div>
            ))}
          </div>
          <div className='text-muted-foreground mt-2 flex justify-between text-[10px]'>
            <span>
              {timeline.length > 0
                ? formatBucket(timeline[0].ts, range.granularity)
                : ''}
            </span>
            <span>
              {timeline.length > 0
                ? formatBucket(
                    timeline.at(-1)?.ts ?? 0,
                    range.granularity
                  )
                : ''}
            </span>
          </div>
        </div>

        <div className='bg-card/60 overflow-x-auto rounded-lg border'>
          {groupBy === 'channel_model' ? (
            <table className='w-full text-sm'>
              <thead>
                <tr className='text-muted-foreground border-b text-left'>
                  <th className='px-4 py-2 font-medium'>{t('Channel')}</th>
                  <th className='px-4 py-2 font-medium'>{t('Model')}</th>
                  <th className='px-4 py-2 text-right font-medium'>
                    {t('Requests')}
                  </th>
                  <th className='px-4 py-2 text-right font-medium'>
                    {t('Token count')}
                  </th>
                  <th className='px-4 py-2 text-right font-medium'>
                    {t('Upstream spend')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {channelModelRows.map((row) => (
                  <tr key={row.key} className='border-b last:border-0'>
                    <td className='px-4 py-2 font-mono'>{row.channel}</td>
                    <td className='px-4 py-2 font-mono'>{row.model}</td>
                    <td className='px-4 py-2 text-right font-mono tabular-nums'>
                      {formatNumber(row.requests)}
                    </td>
                    <td className='px-4 py-2 text-right font-mono tabular-nums'>
                      {formatNumber(row.tokens)}
                    </td>
                    <td className='px-4 py-2 text-right font-mono tabular-nums'>
                      {formatCost(row.cost)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <table className='w-full text-sm'>
              <thead>
                <tr className='text-muted-foreground border-b text-left'>
                  <th className='px-4 py-2 font-medium'>
                    {t(
                      visibleGroupOptions.find((o) => o.value === groupBy)
                        ?.label ?? 'Group'
                    )}
                  </th>
                  <th className='px-4 py-2 text-right font-medium'>
                    {t('Upstream spend')}
                  </th>
                  <th className='px-4 py-2 text-right font-medium'>
                    {t('Requests')}
                  </th>
                  <th className='px-4 py-2 text-right font-medium'>
                    {t('Token count')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {distribution.map((row) => (
                  <tr key={row.key} className='border-b last:border-0'>
                    <td className='px-4 py-2 font-mono'>{row.key}</td>
                    <td className='px-4 py-2 text-right font-mono tabular-nums'>
                      {formatCost(row.cost)}
                    </td>
                    <td className='px-4 py-2 text-right font-mono tabular-nums'>
                      {formatNumber(row.requests)}
                    </td>
                    <td className='px-4 py-2 text-right font-mono tabular-nums'>
                      {formatNumber(row.tokens)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </>
    )
  }

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <Tabs
          value={rangeKey}
          onValueChange={(value) => setRangeKey(value as typeof rangeKey)}
        >
          <TabsList>
            {RANGE_OPTIONS.map((option) => (
              <TabsTrigger key={option.value} value={option.value}>
                {t(option.label)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <Select
          value={groupBy}
          onValueChange={(value) => setGroupBy(value as PBRStatsGroupBy)}
        >
          <SelectTrigger className='w-[140px]'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {visibleGroupOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {t(option.label)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
        {statCard('上游花费', formatCost(totals.cost))}
        {statCard('请求数', formatNumber(totals.requests))}
        {statCard('成功率', `${successRate.toFixed(1)}%`)}
        {statCard('Token 数', formatNumber(totals.tokens))}
      </div>

      {body}
    </div>
  )
}

// 成本统计分节：默认按渠道看上游花费。
export function CostDashboard() {
  return <PbrAnalyticsDashboard />
}

// 模型分析分节：按模型看请求/token/花费。
export function ModelAnalytics() {
  return (
    <PbrAnalyticsDashboard
      defaultGroupBy='model'
      groupOptions={['model', 'channel', 'lane']}
    />
  )
}
