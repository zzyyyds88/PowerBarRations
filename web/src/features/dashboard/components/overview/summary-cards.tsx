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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StaggerContainer, StaggerItem } from '@/components/page-transition'
import { Skeleton } from '@/components/ui/skeleton'
import {
  getPBRStats,
  type PBRStatBucket,
} from '@/features/dashboard/pbr-stats-api'
import { cn } from '@/lib/utils'

// 概览数字卡：PBR 口径（请求数/成功率/token/上游花费），数据来自 GET /api/stats。
// 旧 new-api 的"余额/额度"卡依赖已删的 /api/data*，已由这套指标取代。

const SUMMARY_BUCKETS = 24
const SUMMARY_SECONDS = 24 * 3600
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

function Sparkline(props: { values: number[] }) {
  const max = props.values.reduce((m, v) => Math.max(m, v), 0)
  return (
    <div className={cn('flex h-8 items-end gap-0.5', 'mt-2')}>
      {props.values.map((value, index) => (
        <div
          // eslint-disable-next-line react/no-array-index-key -- fixed-length sparkline
          key={index}
          className='bg-foreground/25 flex-1 rounded-sm'
          style={{
            height: max > 0 ? `${Math.max(2, (value / max) * 100)}%` : '2px',
          }}
        />
      ))}
    </div>
  )
}

function StatBlock(props: { label: string; value: string; values?: number[] }) {
  return (
    <div className='bg-card/60 rounded-lg border px-4 py-3'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 font-mono text-lg font-semibold tabular-nums'>
        {props.value}
      </div>
      {props.values && props.values.length > 0 && (
        <Sparkline values={props.values} />
      )}
    </div>
  )
}

export function SummaryCards() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['dashboard', 'overview', 'pbr-summary'],
    queryFn: async () => {
      // eslint-disable-next-line react/purity -- query functions run outside render
      const to = Math.floor(Date.now() / 1000)
      const from = to - SUMMARY_SECONDS
      const res = await getPBRStats({
        granularity: 'hour',
        from,
        to,
        groupBy: 'lane',
      })
      return { items: res.items ?? [], to }
    },
    staleTime: 60 * 1000,
    refetchOnWindowFocus: false,
  })

  const items = query.data?.items ?? EMPTY_BUCKETS
  const to = query.data?.to ?? 0

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

  const series = useMemo(() => {
    if (to <= 0) return []
    const buckets = Array.from({ length: SUMMARY_BUCKETS }, (_, index) => {
      const ts = to - (SUMMARY_BUCKETS - 1 - index) * 3600
      return { ts: Math.floor(ts / 3600) * 3600, cost: 0, requests: 0 }
    })
    const indexByTs = new Map(
      buckets.map((bucket, index) => [bucket.ts, index])
    )
    for (const item of items) {
      const index = indexByTs.get(Math.floor(item.bucket_ts / 3600) * 3600)
      if (index === undefined) continue
      buckets[index].cost += item.estimated_cost || 0
      buckets[index].requests += item.requests || 0
    }
    return buckets
  }, [items, to])

  const successRate =
    totals.requests > 0 ? (totals.successes / totals.requests) * 100 : 0

  if (query.isLoading) {
    return (
      <section className='space-y-3'>
        <h2 className='text-base font-semibold'>{t('Usage at a glance')}</h2>
        <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
          {['a', 'b', 'c', 'd'].map((key) => (
            <Skeleton key={key} className='h-[92px] rounded-lg' />
          ))}
        </div>
      </section>
    )
  }

  return (
    <section className='space-y-3'>
      <h2 className='text-base font-semibold'>{t('Usage at a glance')}</h2>
      <StaggerContainer className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
        <StaggerItem>
          <StatBlock
            label={`${t('Upstream spend')} · 24h`}
            value={formatCost(totals.cost)}
            values={series.map((bucket) => bucket.cost)}
          />
        </StaggerItem>
        <StaggerItem>
          <StatBlock
            label={`${t('Requests')} · 24h`}
            value={formatNumber(totals.requests)}
            values={series.map((bucket) => bucket.requests)}
          />
        </StaggerItem>
        <StaggerItem>
          <StatBlock
            label={`${t('Success rate')} · 24h`}
            value={`${successRate.toFixed(1)}%`}
          />
        </StaggerItem>
        <StaggerItem>
          <StatBlock
            label={`${t('Token count')} · 24h`}
            value={formatNumber(totals.tokens)}
          />
        </StaggerItem>
      </StaggerContainer>
    </section>
  )
}
