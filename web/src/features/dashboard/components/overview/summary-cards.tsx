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
import {
  getPBRStats,
  type PBRStatBucket,
} from '@/features/dashboard/pbr-stats-api'

const SUMMARY_BUCKETS = 24
const SUMMARY_SECONDS = 24 * 3600
const EMPTY_BUCKETS: PBRStatBucket[] = []

type LaneUsage = {
  lane: string
  requests: number
  successes: number
  promptTokens: number
  completionTokens: number
  cost: number
}

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
    <div aria-hidden='true' className='mt-auto flex h-7 items-end gap-0.5 pt-1'>
      {props.values.map((value, index) => (
        <div
          // eslint-disable-next-line react/no-array-index-key -- fixed-length sparkline
          key={index}
          className='bg-primary/35 flex-1 rounded-sm'
          style={{
            height: max > 0 ? `${Math.max(8, (value / max) * 100)}%` : '8%',
          }}
        />
      ))}
    </div>
  )
}

function StatBlock(props: {
  label: string
  value: string
  detail: string
  values: number[]
}) {
  return (
    <div className='bg-card rounded-panel shadow-panel flex h-[132px] min-w-0 flex-col border px-3 py-2.5 sm:px-4'>
      <div className='text-muted-foreground min-h-8 text-xs leading-4'>
        {props.label}
      </div>
      <div className='mt-0.5 truncate font-mono text-lg font-semibold tabular-nums'>
        {props.value}
      </div>
      <div className='text-muted-foreground truncate text-[11px] leading-4'>
        {props.detail}
      </div>
      <Sparkline values={props.values} />
    </div>
  )
}

function LaneUsageTable(props: { rows: LaneUsage[] }) {
  const { t } = useTranslation()

  return (
    <section className='bg-card rounded-panel shadow-panel overflow-hidden border'>
      <div className='flex min-h-12 flex-wrap items-center justify-between gap-2 border-b px-3 py-2.5 sm:px-4'>
        <h3 className='text-sm font-semibold'>{t('Usage by lane')}</h3>
        <span className='text-muted-foreground text-xs'>
          {t('Last 24 hours')}
        </span>
      </div>
      {props.rows.length === 0 ? (
        <p className='text-muted-foreground px-4 py-8 text-center text-sm'>
          {t('No usage in the last 24 hours')}
        </p>
      ) : (
        <div className='max-h-80 overflow-auto'>
          <Table className='min-w-[620px]'>
            <TableHeader className='bg-card sticky top-0 z-10'>
              <TableRow className='hover:bg-transparent'>
                <TableHead>{t('Lane')}</TableHead>
                <TableHead className='text-right'>{t('Requests')}</TableHead>
                <TableHead className='text-right'>
                  {t('Success rate')}
                </TableHead>
                <TableHead className='text-right'>{t('Token count')}</TableHead>
                <TableHead className='text-right'>
                  {t('Upstream spend')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {props.rows.map((row) => {
                const successRate =
                  row.requests > 0 ? (row.successes / row.requests) * 100 : 0
                return (
                  <TableRow key={row.lane}>
                    <TableCell
                      className='max-w-64 truncate font-medium'
                      title={row.lane}
                    >
                      {row.lane}
                    </TableCell>
                    <TableCell className='text-right'>
                      {formatNumber(row.requests)}
                    </TableCell>
                    <TableCell className='text-right'>
                      {successRate.toFixed(1)}%
                    </TableCell>
                    <TableCell className='text-right'>
                      <span>
                        {formatNumber(row.promptTokens + row.completionTokens)}
                      </span>
                      <span className='text-muted-foreground ml-1 text-xs'>
                        {t('Input tokens')} {formatNumber(row.promptTokens)} /{' '}
                        {t('Output tokens')}{' '}
                        {formatNumber(row.completionTokens)}
                      </span>
                    </TableCell>
                    <TableCell className='text-right'>
                      {formatCost(row.cost)}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}
    </section>
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
          acc.promptTokens += item.prompt_tokens || 0
          acc.completionTokens += item.completion_tokens || 0
          return acc
        },
        {
          cost: 0,
          requests: 0,
          successes: 0,
          promptTokens: 0,
          completionTokens: 0,
        }
      ),
    [items]
  )

  const series = useMemo(() => {
    if (to <= 0) return []
    const buckets = Array.from({ length: SUMMARY_BUCKETS }, (_, index) => {
      const ts = to - (SUMMARY_BUCKETS - 1 - index) * 3600
      return {
        ts: Math.floor(ts / 3600) * 3600,
        cost: 0,
        requests: 0,
        successes: 0,
        tokens: 0,
      }
    })
    const indexByTs = new Map(
      buckets.map((bucket, index) => [bucket.ts, index])
    )
    for (const item of items) {
      const index = indexByTs.get(Math.floor(item.bucket_ts / 3600) * 3600)
      if (index === undefined) continue
      buckets[index].cost += item.estimated_cost || 0
      buckets[index].requests += item.requests || 0
      buckets[index].successes += item.successes || 0
      buckets[index].tokens +=
        (item.prompt_tokens || 0) + (item.completion_tokens || 0)
    }
    return buckets
  }, [items, to])

  const laneUsage = useMemo(() => {
    const byLane = new Map<string, LaneUsage>()
    for (const item of items) {
      const row = byLane.get(item.group) ?? {
        lane: item.group,
        requests: 0,
        successes: 0,
        promptTokens: 0,
        completionTokens: 0,
        cost: 0,
      }
      row.requests += item.requests || 0
      row.successes += item.successes || 0
      row.promptTokens += item.prompt_tokens || 0
      row.completionTokens += item.completion_tokens || 0
      row.cost += item.estimated_cost || 0
      byLane.set(item.group, row)
    }
    return [...byLane.values()].sort(
      (left, right) =>
        right.requests - left.requests || left.lane.localeCompare(right.lane)
    )
  }, [items])

  const successRate =
    totals.requests > 0 ? (totals.successes / totals.requests) * 100 : 0
  const failedRequests = Math.max(0, totals.requests - totals.successes)
  const tokens = totals.promptTokens + totals.completionTokens

  if (query.isLoading) {
    return (
      <section className='flex flex-col gap-3'>
        <h2 className='text-base font-semibold'>{t('Usage at a glance')}</h2>
        <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
          {['a', 'b', 'c', 'd'].map((key) => (
            <Skeleton key={key} className='h-[132px] rounded-lg' />
          ))}
        </div>
        <Skeleton className='h-48 rounded-lg' />
      </section>
    )
  }

  if (query.isError) {
    return (
      <section className='flex flex-col gap-3'>
        <h2 className='text-base font-semibold'>{t('Usage at a glance')}</h2>
        <div
          role='alert'
          className='border-destructive/30 flex flex-wrap items-center justify-between gap-3 rounded-lg border px-3 py-2.5'
        >
          <p className='text-destructive text-sm'>{t('Failed to load')}</p>
          <Button
            variant='outline'
            size='sm'
            onClick={() => void query.refetch()}
          >
            {t('Retry')}
          </Button>
        </div>
      </section>
    )
  }

  return (
    <section className='flex flex-col gap-3'>
      <h2 className='text-base font-semibold'>{t('Usage at a glance')}</h2>
      <StaggerContainer className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
        <StaggerItem>
          <StatBlock
            label={`${t('Upstream spend')} · 24h`}
            value={formatCost(totals.cost)}
            detail={t('Derived from configured upstream prices')}
            values={series.map((bucket) => bucket.cost)}
          />
        </StaggerItem>
        <StaggerItem>
          <StatBlock
            label={`${t('Requests')} · 24h`}
            value={formatNumber(totals.requests)}
            detail={`${t('Successful requests')} ${formatNumber(totals.successes)} · ${t('Failed requests')} ${formatNumber(failedRequests)}`}
            values={series.map((bucket) => bucket.requests)}
          />
        </StaggerItem>
        <StaggerItem>
          <StatBlock
            label={`${t('Success rate')} · 24h`}
            value={`${successRate.toFixed(1)}%`}
            detail={`${formatNumber(totals.successes)} / ${formatNumber(totals.requests)} ${t('Requests').toLowerCase()}`}
            values={series.map((bucket) =>
              bucket.requests > 0
                ? (bucket.successes / bucket.requests) * 100
                : 0
            )}
          />
        </StaggerItem>
        <StaggerItem>
          <StatBlock
            label={`${t('Token count')} · 24h`}
            value={formatNumber(tokens)}
            detail={`${t('Input tokens')} ${formatNumber(totals.promptTokens)} · ${t('Output tokens')} ${formatNumber(totals.completionTokens)}`}
            values={series.map((bucket) => bucket.tokens)}
          />
        </StaggerItem>
      </StaggerContainer>
      <LaneUsageTable rows={laneUsage} />
    </section>
  )
}
