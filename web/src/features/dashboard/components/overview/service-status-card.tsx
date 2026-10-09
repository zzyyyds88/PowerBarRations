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
// 首页概览「服务状态」卡（ui-spec §6.2A 第 3 块，下一行左半，卡内滚动约 300px）。
//
// 逐模型一行：模型名 + 平均延迟（avg_total_ms）+ 吞吐量（output_tps，后缀 tps）
// + 成功率（彩色环指示，success_rate 0~1）。数据源 GET /api/model-status。
// 空样本显示 `-`（不是 0）。卡片内滚动，不撑破页面。

import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { getSuccessRateColor } from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import {
  formatLatencyMs,
  formatSuccessRate,
  formatThroughputTps,
} from '../../lib/format'
import type { PBRResolvedTimeRange } from '../../lib/time-range'
import { getPBRModelStatus } from '../../model-status-api'
import { PanelWrapper } from '../ui/panel-wrapper'

const MAX_ROWS = 50
const STALE_TIME_MS = 60 * 1000
/** 窄屏两列、宽屏四列（模型名 + 三个指标），保证窄屏也能读全。 */
const ROW_GRID =
  'grid grid-cols-2 items-center gap-x-3 gap-y-0.5 sm:grid-cols-[minmax(0,1fr)_6rem_6.5rem_6.5rem]'

/** 成功率彩色环（0~1 小数）；环 + 百分比文字，不靠文字单打独斗。 */
function SuccessRing(props: { rate: number }) {
  const { t } = useTranslation()
  const rate = Number.isFinite(props.rate)
    ? Math.min(1, Math.max(0, props.rate))
    : 0
  const radius = 9
  const circumference = 2 * Math.PI * radius
  return (
    <span
      role='img'
      aria-label={`${t('Success rate')} ${formatSuccessRate(props.rate)}`}
      className='inline-flex size-6 shrink-0 items-center justify-center'
    >
      <svg viewBox='0 0 24 24' className='size-6 -rotate-90'>
        <circle
          cx='12'
          cy='12'
          r={radius}
          fill='none'
          strokeWidth='3'
          className='stroke-muted-foreground/25'
        />
        <circle
          cx='12'
          cy='12'
          r={radius}
          fill='none'
          strokeWidth='3'
          strokeLinecap='round'
          stroke={getSuccessRateColor(rate * 100)}
          strokeDasharray={`${circumference * rate} ${circumference}`}
        />
      </svg>
    </span>
  )
}

function MetricCell(props: { label: string; value: string }) {
  return (
    <span className='flex items-center gap-1 text-xs sm:justify-end'>
      <span className='text-muted-foreground text-[10px] sm:hidden'>
        {props.label}
      </span>
      <span className='font-mono tabular-nums'>{props.value}</span>
    </span>
  )
}

export function ServiceStatusCard(props: { range: PBRResolvedTimeRange }) {
  const { t } = useTranslation()
  const range = props.range

  const query = useQuery({
    queryKey: ['dashboard', 'overview', 'model-status', range.from, range.to],
    queryFn: () => getPBRModelStatus({ from: range.from, to: range.to }),
    staleTime: STALE_TIME_MS,
    refetchOnWindowFocus: false,
  })

  const rows = (query.data?.items ?? []).slice(0, MAX_ROWS)
  const isEmpty = !query.isLoading && !query.isError && rows.length === 0

  let body = (
    <div className='max-h-[300px] overflow-y-auto'>
      <div
        aria-hidden='true'
        className={cn(
          ROW_GRID,
          'text-muted-foreground hidden border-b pb-1.5 text-[11px] font-medium sm:grid'
        )}
      >
        <span>{t('Model')}</span>
        <span className='text-right'>{t('Average latency')}</span>
        <span className='text-right'>{t('Throughput (tps)')}</span>
        <span className='text-right'>{t('Success rate')}</span>
      </div>
      <ul className='divide-border divide-y' aria-label={t('Service status')}>
        {rows.map((item) => (
          <li key={item.model} className={cn(ROW_GRID, 'py-2')}>
            <span
              className='col-span-2 min-w-0 truncate font-mono text-xs sm:col-span-1'
              title={item.model}
            >
              {item.model}
            </span>
            <MetricCell
              label={t('Average latency')}
              value={formatLatencyMs(item.avg_total_ms)}
            />
            <MetricCell
              label={t('Throughput (tps)')}
              value={formatThroughputTps(item.output_tps)}
            />
            <span className='flex items-center justify-start gap-1.5 sm:justify-end'>
              <SuccessRing rate={item.success_rate} />
              <span className='font-mono text-xs tabular-nums'>
                {formatSuccessRate(item.success_rate)}
              </span>
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
  if (query.isError) {
    body = (
      <ErrorState
        className='min-h-[200px]'
        title={t('Failed to load')}
        onRetry={() => void query.refetch()}
      />
    )
  }

  return (
    <PanelWrapper
      title={t('Service status')}
      loading={query.isLoading}
      empty={isEmpty}
      emptyMessage={t('No data in the selected range')}
      height='h-[300px]'
    >
      {body}
    </PanelWrapper>
  )
}
