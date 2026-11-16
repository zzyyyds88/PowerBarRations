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
import { formatTokenAmount } from '@/lib/format'

import type { PBRResolvedTimeRange } from '../../lib/time-range'
import { getPBRStats, PBR_CHANNEL_MODEL_SEPARATOR } from '../../pbr-stats-api'

// 分组统计表（ui-spec §6.2，2026-10-25 一屏重排）：数据看板唯一的分组统计卡。
//
// 原先「模型调用分析」「成本统计」两个分节各有数字卡 + 时间线图 + 表格，
// 与概览的指标条/趋势图大量重复；合并为**一张表**：维度下拉覆盖
// 渠道/模型/车道/令牌/令牌×模型/渠道×模型，排序切换（按调用次数 / 按上游
// 花费）兼管两个视角。数字摘要由概览「模型健康」面板承担，趋势图由模型分析卡承担。
// 时间区间由看板顶层统一下发（三块卡片共用）。

const GROUP_OPTIONS: { value: PBRStatsGroupBy; label: string }[] = [
  { value: 'channel', label: 'By channel' },
  { value: 'model', label: 'By model' },
  { value: 'lane', label: 'By lane' },
  { value: 'key', label: 'By key' },
  { value: 'key_model', label: 'Key × Model' },
  { value: 'channel_model', label: 'Channel × Model' },
]

type PBRStatsBucket = {
  bucket_ts: number
  group: string
  requests: number
  successes: number
  prompt_tokens: number
  completion_tokens: number
  estimated_cost: number
}

type PBRStatsGroupBy =
  | 'lane'
  | 'channel'
  | 'key'
  | 'model'
  | 'channel_model'
  | 'key_model'

const EMPTY_BUCKETS: PBRStatsBucket[] = []

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

type SortMetric = 'requests' | 'cost' | 'tokens'

export function StatsTableCard(props: { range: PBRResolvedTimeRange }) {
  const { t } = useTranslation()
  const range = props.range
  const [groupBy, setGroupBy] = useState<PBRStatsGroupBy>('channel')
  const [sortBy, setSortBy] = useState<SortMetric>('requests')

  const query = useQuery({
    queryKey: [
      'pbr-analytics-stats',
      range.preset,
      range.from,
      range.to,
      groupBy,
    ],
    queryFn: () =>
      getPBRStats({
        granularity: range.granularity,
        from: range.from,
        to: range.to,
        groupBy,
      }),
    refetchOnWindowFocus: false,
  })

  const items = query.data?.items ?? EMPTY_BUCKETS

  const rows = useMemo(() => {
    const byGroup = new Map<
      string,
      {
        cost: number
        requests: number
        successes: number
        promptTokens: number
        completionTokens: number
      }
    >()
    for (const item of items) {
      const key = item.group || t('Not recorded')
      const current = byGroup.get(key) ?? {
        cost: 0,
        requests: 0,
        successes: 0,
        promptTokens: 0,
        completionTokens: 0,
      }
      current.cost += item.estimated_cost || 0
      current.requests += item.requests || 0
      current.successes += item.successes || 0
      current.promptTokens += item.prompt_tokens || 0
      current.completionTokens += item.completion_tokens || 0
      byGroup.set(key, current)
    }
    return [...byGroup.entries()]
      .map(([key, value]) => ({
        key,
        successRate: value.requests > 0 ? value.successes / value.requests : 0,
        ...value,
        totalTokens: value.promptTokens + value.completionTokens,
      }))
      .sort((a, b) => {
        if (sortBy === 'tokens') return b.totalTokens - a.totalTokens
        return sortBy === 'requests' ? b.requests - a.requests : b.cost - a.cost
      })
  }, [items, t, sortBy])

  const isPair = groupBy === 'channel_model' || groupBy === 'key_model'
  // 第二列表头（单层判定）：lane 维度就是车道名本身；pair 维度是模型；
  // 其余维度复用分组下拉的 label。
  let modelColumnLabel: string
  if (groupBy === 'lane') {
    modelColumnLabel = t('Lane')
  } else if (isPair) {
    modelColumnLabel = t('Model')
  } else {
    modelColumnLabel = t(
      GROUP_OPTIONS.find((o) => o.value === groupBy)?.label ?? 'Group'
    )
  }

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
  } else if (rows.length === 0) {
    body = (
      <EmptyState
        title={t('No call data yet')}
        description={t(
          'Once requests flow through lanes, call counts, success rate and token usage appear here.'
        )}
      />
    )
  } else {
    body = (
      <div className='max-h-[19rem] overflow-y-auto'>
        <table className='w-full text-sm'>
          <thead>
            <tr className='text-muted-foreground bg-card sticky top-0 z-10 border-b [background-image:var(--surface-card)] text-left text-xs'>
              {isPair && (
                <th className='px-3 py-2 font-medium'>
                  {groupBy === 'channel_model' ? t('Channel') : t('Client key')}
                </th>
              )}
              <th className='px-3 py-2 font-medium'>{modelColumnLabel}</th>
              <th className='px-3 py-2 text-right font-medium'>
                {t('Call count')}
              </th>
              <th className='px-3 py-2 text-right font-medium'>
                {t('Success rate')}
              </th>
              <th className='px-3 py-2 text-right font-medium'>
                {t('Token count')}
              </th>
              <th className='px-3 py-2 text-right font-medium'>
                {t('Upstream spend')}
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const [first, second] = isPair
                ? row.key.split(PBR_CHANNEL_MODEL_SEPARATOR)
                : [row.key, '']
              return (
                <tr key={row.key} className='border-b last:border-0'>
                  {isPair && (
                    <td className='px-3 py-1.5 font-mono'>
                      {first || t('Not recorded')}
                    </td>
                  )}
                  <td className='px-3 py-1.5 font-mono'>
                    {isPair ? (second ?? '') : row.key}
                  </td>
                  <td className='px-3 py-1.5 text-right font-mono tabular-nums'>
                    {formatNumber(row.requests)}
                  </td>
                  <td className='px-3 py-1.5 text-right font-mono tabular-nums'>
                    {(row.successRate * 100).toFixed(1)}%
                  </td>
                  <td className='px-3 py-1.5 text-right font-mono tabular-nums'>
                    {formatTokenAmount(row.totalTokens)}
                  </td>
                  <td className='px-3 py-1.5 text-right font-mono tabular-nums'>
                    {formatCost(row.cost)}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    )
  }

  return (
    <div className='border-border shadow-panel rounded-control bg-card flex h-full min-h-0 flex-col border [background-image:var(--surface-card)] p-3'>
      <div className='mb-2 flex flex-wrap items-center gap-2'>
        <span className='text-sm font-medium'>{t('Statistics')}</span>
        {/* 排序切换：按调用次数 / 按上游花费，兼管调用分析与成本两个视角。 */}
        <div className='ml-auto flex items-center gap-1'>
          <button
            type='button'
            onClick={() => setSortBy('requests')}
            aria-pressed={sortBy === 'requests'}
            className={cnSort(sortBy === 'requests')}
          >
            {t('By call count')}
          </button>
          <button
            type='button'
            onClick={() => setSortBy('cost')}
            aria-pressed={sortBy === 'cost'}
            className={cnSort(sortBy === 'cost')}
          >
            {t('By spend')}
          </button>
          <button
            type='button'
            onClick={() => setSortBy('tokens')}
            aria-pressed={sortBy === 'tokens'}
            className={cnSort(sortBy === 'tokens')}
          >
            {t('By tokens')}
          </button>
        </div>
        <Select
          items={GROUP_OPTIONS.map((option) => ({
            value: option.value,
            label: t(option.label),
          }))}
          value={groupBy}
          onValueChange={(value) => setGroupBy(value as PBRStatsGroupBy)}
        >
          <SelectTrigger className='w-[150px]'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {GROUP_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {t(option.label)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className='min-h-0 flex-1'>{body}</div>
    </div>
  )
}

function cnSort(active: boolean): string {
  return [
    'rounded-full px-2.5 py-1 text-xs transition-colors',
    active
      ? 'bg-primary text-primary-foreground'
      : 'text-muted-foreground hover:bg-muted',
  ].join(' ')
}
