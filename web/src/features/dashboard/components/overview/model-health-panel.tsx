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
// 首页概览「模型健康」面板（ui-spec §6 概览）。
//
// 把原先三块内容合并成一张统一面板：
// - 顶部摘要：折算成本 / Token 总量（窗口口径 + 环比）
// - 主体表格：模型 / 请求数 / 延迟 p50（p95 悬浮）/ 干净样本 TPS /
//   输出速度 / 实时流量 / 成功率
//
// 数据源：
// - 成功率、请求数、延迟、输出速度：`/api/model-status`（跟随页头时间区间）
// - 实时流量：同一接口的近 15 分钟窗口，60s 刷新，页面隐藏时暂停
// - 成本/Token 摘要：`/api/stats?group_by=model`（与模型分析共用查询键）
//
// 「输出速度」只统计成功且一次就成功的样本（排除故障转移的等待），流式再扣掉首字
// 等待，回答"这个模型吐字有多快"；没有样本时显示 `—`，原因放 tooltip。
// 「实时流量」是近 15 分钟总 Token/秒（输入 + 输出），回答"现在处理多少量"。
// 两列一个是速度、一个是流量，口径不同，不能合并。

import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { ProgressRing } from '@/components/charts'
import { ErrorState } from '@/components/error-state'
import { formatCostYuan } from '@/features/keys/lib/cost'
import { getSuccessRateColor } from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import {
  deltaPercent,
  formatCount,
  formatDeltaPercent,
  formatLatencyMs,
  formatSuccessRate,
  formatTokenAmount,
} from '../../lib/format'
import { sumWindowTotals } from '../../lib/metrics'
import {
  overviewPreviousStatsQueryKey,
  overviewStatsQueryKey,
} from '../../lib/overview-query'
import type { PBRResolvedTimeRange } from '../../lib/time-range'
import { getPBRModelStatus } from '../../model-status-api'
import {
  getPBRStats,
  type PBRStatBucket,
  type PBRStatsGroupBy,
} from '../../pbr-stats-api'
import { PanelTotal } from '../ui/panel-total'
import { PanelWrapper } from '../ui/panel-wrapper'

const STALE_TIME_MS = 60 * 1000
const THROUGHPUT_WINDOW_SECONDS = 15 * 60
const THROUGHPUT_REFRESH_MS = 60 * 1000
const MAX_ROWS = 50
const EMPTY_BUCKETS: PBRStatBucket[] = []
const WINDOW_GROUP_BY: PBRStatsGroupBy = 'model'

const ROW_GRID =
  'grid grid-cols-2 items-center gap-x-3 gap-y-0.5 sm:grid-cols-[minmax(0,1fr)_5rem_8rem_7rem_7rem_7rem]'

/** 每秒 token 数的统一显示；无样本返回 null。 */
function formatRate(perSecond: number): string | null {
  if (!Number.isFinite(perSecond) || perSecond <= 0) return null
  if (perSecond >= 1000) return `${(perSecond / 1000).toFixed(1)}k tok/s`
  if (perSecond >= 10) return `${perSecond.toFixed(1)} tok/s`
  if (perSecond < 0.01) return '<0.01 tok/s'
  return `${perSecond.toFixed(2)} tok/s`
}

/** 近 15 分钟总 token 速度：窗口内 tokens_total / 900，单位 tok/s。 */
function formatTokenRate(tokensTotal: number): string | null {
  return formatRate(tokensTotal / THROUGHPUT_WINDOW_SECONDS)
}

function SummaryStat(props: {
  label: string
  value: string
  delta: number | null
  invertDelta?: boolean
  title?: string
}) {
  const { t } = useTranslation()
  let tone = 'text-muted-foreground'
  let arrow = ''
  if (props.delta != null && props.delta !== 0) {
    const positive = props.delta > 0
    const good = props.invertDelta ? !positive : positive
    tone = good
      ? 'text-[var(--tone-success-text)]'
      : 'text-[var(--tone-destructive-text)]'
    arrow = positive ? '↑' : '↓'
  }
  return (
    <div className='min-w-0' title={props.title}>
      <div className='text-muted-foreground truncate text-xs font-medium uppercase'>
        {props.label}
      </div>
      <div className='mt-0.5 truncate font-mono text-lg font-semibold tabular-nums'>
        {props.value}
      </div>
      <div className={cn('mt-0.5 flex items-center gap-1 text-xs', tone)}>
        <span aria-hidden='true'>{arrow}</span>
        <span className='shrink-0 tabular-nums'>
          {formatDeltaPercent(props.delta)}
        </span>
        <span className='text-muted-foreground truncate'>
          {t('vs previous period')}
        </span>
      </div>
    </div>
  )
}

function MetricCell(props: { label: string; value: string; title?: string }) {
  return (
    <span
      className='flex min-w-0 items-center gap-1 text-xs sm:justify-end'
      title={props.title}
    >
      <span className='text-muted-foreground text-xs sm:hidden'>
        {props.label}
      </span>
      <span className='truncate font-mono tabular-nums'>{props.value}</span>
    </span>
  )
}

function SuccessCell(props: { rate: number }) {
  const { t } = useTranslation()
  const rate = Number.isFinite(props.rate)
    ? Math.min(1, Math.max(0, props.rate))
    : 0
  return (
    <span className='flex min-w-0 items-center gap-1.5 sm:justify-end'>
      <span className='text-muted-foreground text-xs sm:hidden'>
        {t('Success rate')}
      </span>
      <ProgressRing
        value={rate}
        color={getSuccessRateColor(rate * 100)}
        ariaLabel={`${t('Success rate')} ${formatSuccessRate(props.rate)}`}
      />
      <span className='font-mono text-xs tabular-nums'>
        {formatSuccessRate(props.rate)}
      </span>
    </span>
  )
}

export function ModelHealthPanel(props: { range: PBRResolvedTimeRange }) {
  const { t, i18n } = useTranslation()
  const range = props.range

  // 首页只展示对外模型：group_by=lane 让后端按路由键聚合，不暴露上游真名。
  const statusQuery = useQuery({
    queryKey: [
      'dashboard',
      'overview',
      'model-status',
      'lane',
      range.from,
      range.to,
    ],
    queryFn: () =>
      getPBRModelStatus({
        from: range.from,
        to: range.to,
        groupBy: 'lane',
      }),
    staleTime: STALE_TIME_MS,
    refetchOnWindowFocus: false,
  })

  // 实时流量单独取近 15 分钟：成功率/请求数跟随页头时间区间，流量保持“当前”
  // 语义，避免 24h 平均值被当成此刻速度。每 60s 重取一次，页面隐藏时暂停。
  const throughputQuery = useQuery({
    queryKey: ['dashboard', 'overview', 'model-throughput', '15m'],
    queryFn: () => {
      const to = Math.floor(Date.now() / 1000)
      return getPBRModelStatus({
        from: to - THROUGHPUT_WINDOW_SECONDS,
        to,
        groupBy: 'lane',
      })
    },
    staleTime: THROUGHPUT_REFRESH_MS / 2,
    refetchInterval: THROUGHPUT_REFRESH_MS,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: false,
  })
  const throughputByModel = new Map(
    (throughputQuery.data?.items ?? []).map((item) => [
      item.model,
      item.tokens_total,
    ])
  )

  const statsQuery = useQuery({
    queryKey: overviewStatsQueryKey(WINDOW_GROUP_BY, range),
    queryFn: () =>
      getPBRStats({
        granularity: range.granularity,
        from: range.from,
        to: range.to,
        groupBy: WINDOW_GROUP_BY,
      }),
    staleTime: STALE_TIME_MS,
    refetchOnWindowFocus: false,
  })
  const previousStatsQuery = useQuery({
    queryKey: overviewPreviousStatsQueryKey(WINDOW_GROUP_BY, range),
    queryFn: () =>
      getPBRStats({
        granularity: range.granularity,
        from: range.previousFrom,
        to: range.previousTo,
        groupBy: WINDOW_GROUP_BY,
      }),
    staleTime: STALE_TIME_MS,
    refetchOnWindowFocus: false,
  })
  const statsItems = statsQuery.data?.items ?? EMPTY_BUCKETS
  const previousStatsItems = previousStatsQuery.data?.items ?? EMPTY_BUCKETS
  const totals = useMemo(() => sumWindowTotals(statsItems), [statsItems])
  const previousTotals = useMemo(
    () => sumWindowTotals(previousStatsItems),
    [previousStatsItems]
  )
  const statsReady = statsQuery.data != null

  const rows = (statusQuery.data?.items ?? []).slice(0, MAX_ROWS)
  const isEmpty =
    !statusQuery.isLoading && !statusQuery.isError && rows.length === 0

  let body = (
    <div className='max-h-[320px] overflow-y-auto'>
      <div
        aria-hidden='true'
        className={cn(
          ROW_GRID,
          'text-muted-foreground bg-card sticky top-0 z-10 hidden border-b pb-1.5 text-xs font-medium [background-image:var(--surface-card)] sm:grid'
        )}
      >
        <span>{t('Model')}</span>
        <span className='text-right'>{t('Requests')}</span>
        <span className='text-right'>{t('Latency')}</span>
        <span className='text-right'>{t('Output speed')}</span>
        <span className='text-right'>{t('Live traffic')}</span>
        <span className='text-right'>{t('Success rate')}</span>
      </div>
      <ul className='divide-border divide-y' aria-label={t('Model health')}>
        {rows.map((item) => {
          const outputSpeed = formatRate(item.output_tokens_per_second) ?? '—'
          const liveThroughput =
            formatTokenRate(throughputByModel.get(item.model) ?? 0) ?? '—'
          return (
            <li key={item.model} className={cn(ROW_GRID, 'py-2')}>
              <span
                className='col-span-2 min-w-0 truncate font-mono text-xs sm:col-span-1'
                title={item.model}
              >
                {item.model}
              </span>
              <MetricCell
                label={t('Requests')}
                value={formatCount(item.requests)}
              />
              <MetricCell
                label={t('Latency')}
                value={formatLatencyMs(item.p50_total_ms)}
                title={`p95: ${formatLatencyMs(item.p95_total_ms)}`}
              />
              <MetricCell
                label={t('Output speed')}
                value={outputSpeed}
                title={t(
                  'Output speed = all completion tokens in the window / window seconds.'
                )}
              />
              <MetricCell
                label={t('Live traffic')}
                value={liveThroughput}
                title={t(
                  'Total tokens per second over the last 15 minutes (input + output, including retries).'
                )}
              />
              <SuccessCell rate={item.success_rate} />
            </li>
          )
        })}
      </ul>
    </div>
  )

  if (statusQuery.isError) {
    body = (
      <ErrorState
        className='min-h-[200px]'
        title={t('Failed to load')}
        onRetry={() => void statusQuery.refetch()}
      />
    )
  }

  return (
    <PanelWrapper
      title={t('Model health')}
      description={t('Success rate, latency and live throughput by model.')}
      loading={statusQuery.isLoading}
      empty={isEmpty}
      emptyMessage={t('No data in the selected range')}
      height='h-[240px]'
      headerActions={
        <PanelTotal
          value={
            statusQuery.data == null
              ? null
              : formatCount(statusQuery.data.items.length)
          }
        />
      }
    >
      <div className='space-y-3'>
        <div className='border-border/60 grid grid-cols-2 gap-4 border-b pb-3'>
          <SummaryStat
            label={t('Converted cost')}
            value={statsReady ? formatCostYuan(totals.cost) : '—'}
            delta={
              statsReady ? deltaPercent(totals.cost, previousTotals.cost) : null
            }
            invertDelta
            title={t('Statistical cost')}
          />
          <SummaryStat
            label={t('Total tokens')}
            value={
              statsReady ? formatTokenAmount(totals.tokens, i18n.language) : '—'
            }
            delta={
              statsReady
                ? deltaPercent(totals.tokens, previousTotals.tokens)
                : null
            }
            title={t('Statistical tokens')}
          />
        </div>
        {body}
      </div>
    </PanelWrapper>
  )
}
