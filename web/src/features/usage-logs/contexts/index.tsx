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
import type { ColumnDef } from '@tanstack/react-table'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { WorkspacePage } from '@/components/workspace-page'
import { usePageUrlState } from '@/hooks/use-page-url-state'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { formatTimestampToDate, formatTokens } from '@/lib/format'
import { cn } from '@/lib/utils'

import { CompactDateTimeRangePicker } from '../components/compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterToolbar,
} from '../components/logs-filter-toolbar'
import { PBRContextCard } from '../components/pbr-context-card'
import { getDefaultTimeRange } from '../lib/utils'
import {
  getPBRRequestLog,
  listPBRRequestLogsPaged,
  type PBRRequestLogListItem,
} from '../pbr/pbr-logs-api'

function fillTone(ratio: number): string {
  if (ratio >= 0.9) return 'text-rose-600'
  if (ratio >= 0.75) return 'text-amber-600'
  return 'text-emerald-600'
}

function statusLabel(status: string | undefined, t: (key: string) => string) {
  switch (status) {
    case 'unsupported':
      return t('Not available for this request type')
    case 'too_large':
      return t('Body too large to inspect')
    case 'unavailable':
      return t('Could not read context')
    default:
      return t('Available')
  }
}

function ContextDetailDialog(props: {
  entry: PBRRequestLogListItem | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['pbr-context-detail', props.entry?.id],
    queryFn: () => getPBRRequestLog(props.entry?.id ?? 0),
    enabled: Boolean(props.entry),
    staleTime: 60_000,
  })
  return (
    <Dialog
      open={Boolean(props.entry)}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Context details')}
      description={
        props.entry
          ? `${props.entry.request_model} · ${formatTimestampToDate(Math.floor(Date.parse(props.entry.ts) / 1000))}`
          : undefined
      }
      size='lg'
    >
      {props.entry && (
        <div className='space-y-3'>
          <PBRContextCard
            context={query.data?.context}
            status={query.data?.context_status ?? props.entry.context_status}
            tokens={props.entry.context_tokens}
            window={props.entry.context_window}
            counted={props.entry.context_counted}
          />
        </div>
      )}
    </Dialog>
  )
}

/**
 * Usage › Context (usage-spec §5, ui-spec §6 日志): every recent request's
 * prompt composition and window fill, with the full partition card on open.
 */
export function ContextPage() {
  const { t } = useTranslation()
  const { search, navigate } = usePageUrlState()
  const defaultRange = useMemo(() => getDefaultTimeRange(), [])
  const from = Number(search.startTime) || defaultRange.start.getTime()
  const to = Number(search.endTime) || defaultRange.end.getTime()
  const [selected, setSelected] = useState<PBRRequestLogListItem | null>(null)
  const { pagination, onPaginationChange } = useTableUrlState({
    search,
    navigate,
    pagination: { defaultPage: 1, defaultPageSize: 50 },
  })
  const query = useQuery({
    queryKey: [
      'pbr-contexts',
      from,
      to,
      pagination.pageIndex,
      pagination.pageSize,
    ],
    queryFn: () =>
      listPBRRequestLogsPaged({
        page: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
        context: true,
        since: Math.floor(from / 1000),
        until: Math.floor(to / 1000),
      }),
    staleTime: 30_000,
  })

  const columns = useMemo<ColumnDef<PBRRequestLogListItem>[]>(
    () => [
      {
        id: 'ts',
        header: t('Time'),
        accessorFn: (row) => row.ts,
        cell: ({ row }) =>
          formatTimestampToDate(Math.floor(Date.parse(row.original.ts) / 1000)),
        size: 150,
      },
      {
        id: 'model',
        header: t('Request model'),
        accessorFn: (row) => row.request_model,
        cell: ({ row }) => (
          <span className='font-mono text-xs'>
            {row.original.request_model}
          </span>
        ),
      },
      {
        id: 'session',
        header: t('Session'),
        accessorFn: (row) => row.session_id ?? '',
        cell: ({ row }) =>
          row.original.session_id ? (
            <span className='font-mono text-xs'>
              {row.original.session_id.slice(0, 14)}
            </span>
          ) : (
            <span className='text-muted-foreground'>—</span>
          ),
        size: 150,
      },
      {
        id: 'tokens',
        header: t('Context tokens'),
        accessorFn: (row) => row.context_tokens ?? 0,
        cell: ({ row }) => (
          <span className='font-mono text-xs tabular-nums'>
            {formatTokens(row.original.context_tokens ?? 0)}
          </span>
        ),
        size: 120,
      },
      {
        id: 'fill',
        header: t('Fill'),
        accessorFn: (row) => {
          const tokens = row.context_tokens ?? 0
          const window = row.context_window ?? 0
          return window > 0 ? tokens / window : 0
        },
        cell: ({ row }) => {
          const tokens = row.original.context_tokens ?? 0
          const window = row.original.context_window ?? 0
          if (window <= 0 || tokens <= 0) {
            return <span className='text-muted-foreground'>—</span>
          }
          const ratio = Math.min(1, tokens / window)
          return (
            <span
              className={cn('font-mono text-xs tabular-nums', fillTone(ratio))}
              title={`${formatTokens(tokens)} / ${formatTokens(window)}`}
            >
              {Math.round(ratio * 100)}%
            </span>
          )
        },
        size: 80,
      },
      {
        id: 'status',
        header: t('Status'),
        accessorFn: (row) => row.context_status ?? '',
        cell: ({ row }) => (
          <Badge variant='outline' className='text-xs'>
            {statusLabel(row.original.context_status, t)}
          </Badge>
        ),
        size: 140,
      },
      {
        id: 'open',
        header: '',
        cell: ({ row }) => (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='h-7 px-2 text-xs'
            onClick={() => setSelected(row.original)}
          >
            {t('Details')}
          </Button>
        ),
        enableSorting: false,
        size: 70,
      },
    ],
    [t, setSelected]
  )

  const { table } = useDataTable({
    data: query.data?.items ?? [],
    columns,
    totalCount: query.data?.total ?? 0,
    getRowId: (row) => String(row.id),
    pagination,
    onPaginationChange,
    manualPagination: true,
    manualFiltering: true,
  })

  const toolbar = (
    <LogsFilterToolbar
      table={table}
      compactMobile
      primaryFilters={
        <LogsFilterField>
          <CompactDateTimeRangePicker
            start={new Date(from)}
            end={new Date(to)}
            onChange={({ start, end }) => {
              if (!start || !end) return
              navigate({
                search: {
                  ...search,
                  page: 1,
                  startTime: start.getTime(),
                  endTime: end.getTime(),
                },
              })
            }}
          />
        </LogsFilterField>
      }
      mobilePinnedFilters={null}
      mobileFilters={null}
      hasActiveFilters={false}
      hasAdvancedActiveFilters={false}
      advancedFilterCount={0}
      onSearch={() => void query.refetch()}
      searchLoading={query.isFetching}
      onReset={() => {
        const { start, end } = getDefaultTimeRange()
        navigate({
          search: {
            page: 1,
            startTime: start.getTime(),
            endTime: end.getTime(),
          },
        })
      }}
    />
  )

  return (
    <WorkspacePage
      title={t('Context')}
      description={t('Prompt composition and window fill for recent requests.')}
      fixedContent
    >
      <div className='flex h-full min-h-0 flex-col gap-3'>
        <div className='text-muted-foreground text-xs'>
          {t(
            'Only the prompt composition metadata is kept: names, tags and token counts.'
          )}
        </div>
        <div className='min-h-0 flex-1'>
          <DataTablePage
            table={table}
            columns={columns}
            isLoading={query.isLoading}
            isFetching={query.isFetching}
            isError={query.isError}
            error={query.error}
            onRetry={() => void query.refetch()}
            emptyTitle={t('No context records')}
            emptyDescription={t(
              'Requests with context metadata appear here once new traffic arrives.'
            )}
            skeletonKeyPrefix='pbr-context-skeleton'
            toolbar={toolbar}
            mobileBreakpoint={900}
          />
        </div>
      </div>
      <ContextDetailDialog entry={selected} onClose={() => setSelected(null)} />
    </WorkspacePage>
  )
}
