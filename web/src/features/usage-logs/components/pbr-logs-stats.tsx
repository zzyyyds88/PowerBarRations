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
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { getPBRStats } from '@/features/dashboard/pbr-stats-api'
import { formatCostYuan } from '@/features/keys/lib/cost'
import { cn } from '@/lib/utils'

// 请求日志窗口 KPI（ui-spec §6 日志）：当前时间区间的成本 / 请求数 / RPM / TPM。
//
// 数据源是聚合表 `GET /api/stats?group_by=lane`，与明细日志的裁剪互不影响。
// **刻意不按车道/渠道/密钥等明细筛选收窄**：聚合接口不支持这些维度组合，
// 标签写成“窗口”总量，避免让人误以为这是当前过滤结果（ui-spec §6）。

/** 窗口超过两天时按天取桶，否则按小时；与看板的时间粒度规则一致。 */
const DAY_SECONDS = 86400
const TWO_DAYS_SECONDS = 2 * DAY_SECONDS
const STALE_TIME_MS = 30 * 1000

function StatBadge(props: { label: string; value: string; accent: string }) {
  return (
    <span className='border-border/60 bg-muted/25 inline-flex h-7 items-center gap-2 rounded-md border px-2.5 text-xs shadow-xs'>
      <span className={cn('h-3.5 w-0.5 rounded-full', props.accent)} />
      <span className='text-muted-foreground whitespace-nowrap'>
        {props.label}
      </span>
      <span className='text-foreground/85 font-mono font-semibold tabular-nums'>
        {props.value}
      </span>
    </span>
  )
}

function formatRate(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0'
  return new Intl.NumberFormat(undefined, {
    maximumFractionDigits: value < 10 ? 1 : 0,
  }).format(value)
}

function formatCount(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0'
  return new Intl.NumberFormat(undefined, {
    notation: 'compact',
    maximumFractionDigits: 1,
  }).format(value)
}

export function PBRLogsStats(props: { from: number; to: number }) {
  const { t } = useTranslation()
  const from = Math.max(0, Math.floor(props.from))
  const to = Math.max(from + 1, Math.floor(props.to))
  const granularity = to - from > TWO_DAYS_SECONDS ? 'day' : 'hour'

  const query = useQuery({
    queryKey: ['pbr-logs-window-stats', from, to, granularity],
    queryFn: () => getPBRStats({ granularity, from, to, groupBy: 'lane' }),
    staleTime: STALE_TIME_MS,
    refetchOnWindowFocus: false,
  })

  if (query.isLoading) {
    return (
      <div className='flex flex-wrap items-center gap-2'>
        {[0, 1, 2, 3].map((index) => (
          <Skeleton key={index} className='h-7 w-[112px] rounded-md' />
        ))}
      </div>
    )
  }

  // 取数失败时保留条位但显示真实不可用，不伪装成 0。
  if (query.isError) {
    return (
      <div className='flex flex-wrap items-center gap-2'>
        <span className='text-muted-foreground text-xs'>
          {t('Window totals unavailable')}
        </span>
        <button
          type='button'
          onClick={() => void query.refetch()}
          className='text-muted-foreground hover:text-foreground inline-flex size-7 items-center justify-center rounded-md transition-colors'
          aria-label={t('Retry')}
        >
          <RefreshCw className='size-3.5' aria-hidden='true' />
        </button>
      </div>
    )
  }

  const items = query.data?.items ?? []
  let requests = 0
  let tokens = 0
  let cost = 0
  for (const item of items) {
    requests += item.requests || 0
    tokens += (item.prompt_tokens || 0) + (item.completion_tokens || 0)
    cost += item.estimated_cost || 0
  }
  const minutes = Math.max((to - from) / 60, 1)
  const rpm = requests / minutes
  const tpm = tokens / minutes

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <StatBadge
        label={t('Window cost')}
        value={formatCostYuan(cost)}
        accent='bg-emerald-500/70'
      />
      <StatBadge
        label={t('Window requests')}
        value={formatCount(requests)}
        accent='bg-sky-500/70'
      />
      <StatBadge
        label={t('RPM')}
        value={formatRate(rpm)}
        accent='bg-rose-500/65'
      />
      <StatBadge
        label={t('TPM')}
        value={formatCount(tpm)}
        accent='bg-slate-400/70'
      />
    </div>
  )
}
