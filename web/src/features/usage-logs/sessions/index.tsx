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

import { DataTablePage, useDataTable } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { WorkspacePage } from '@/components/workspace-page'
import { getPBRStats } from '@/features/dashboard/pbr-stats-api'
import { formatCostYuan } from '@/features/keys/lib/cost'
import { usePageUrlState } from '@/hooks/use-page-url-state'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { formatTokens } from '@/lib/format'
import { cn } from '@/lib/utils'

import { CompactDateTimeRangePicker } from '../components/compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from '../components/logs-filter-toolbar'
import { getDefaultTimeRange } from '../lib/utils'
import {
  listPBRSessions,
  type PBRSession,
  type PBRSessionSort,
} from '../pbr/pbr-sessions-api'
import { useSessionColumns } from './components/session-columns'
import { SessionDetailSheet } from './components/session-detail-sheet'

function sortLabel(sort: PBRSessionSort, t: (key: string) => string): string {
  if (sort === 'last_ts') return t('Last activity')
  if (sort === 'first_ts') return t('First seen')
  if (sort === 'requests') return t('Requests')
  if (sort === 'tokens') return t('Tokens')
  return t('Cost')
}

const SESSION_SORT_OPTIONS: PBRSessionSort[] = [
  'last_ts',
  'first_ts',
  'requests',
  'tokens',
  'cost',
]

function readString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function WindowStat(props: { label: string; value: string; tone?: string }) {
  return (
    <span className='border-border/60 bg-muted/25 inline-flex h-7 items-center gap-2 rounded-md border px-2.5 text-xs shadow-xs'>
      <span className='text-muted-foreground whitespace-nowrap'>
        {props.label}
      </span>
      <span
        className={cn(
          'text-foreground/85 font-mono font-semibold tabular-nums',
          props.tone
        )}
      >
        {props.value}
      </span>
    </span>
  )
}

/**
 * Gateway sessions (usage-spec §7, ui-spec §6 日志). A session is derived from
 * requests the gateway saw; it is not a local agent transcript.
 */
export function SessionsPage() {
  const { t } = useTranslation()
  const { search, navigate } = usePageUrlState()
  const defaultRange = useMemo(() => getDefaultTimeRange(), [])
  const from = Number(search.startTime) || defaultRange.start.getTime()
  const to = Number(search.endTime) || defaultRange.end.getTime()
  const client = readString(search.client)
  const model = readString(search.model)
  const lane = readString(search.lane)
  const q = readString(search.q)
  const sort = (readString(search.sort) || 'last_ts') as PBRSessionSort
  const order = readString(search.order) === 'asc' ? 'asc' : 'desc'
  const selectedKey = readString(search.session) || null

  const [draft, setDraft] = useState({ model, lane, q })

  const { pagination, onPaginationChange } = useTableUrlState({
    search,
    navigate,
    pagination: { defaultPage: 1, defaultPageSize: 50 },
  })

  const clientsQuery = useQuery({
    queryKey: ['pbr-session-client-options', from, to],
    queryFn: () =>
      getPBRStats({
        granularity: 'day',
        from: Math.floor(from / 1000),
        to: Math.floor(to / 1000),
        groupBy: 'client',
      }),
    staleTime: 60_000,
    refetchOnWindowFocus: false,
  })
  const clientOptions = useMemo(() => {
    const seen = new Map<string, string>()
    for (const bucket of clientsQuery.data?.items ?? []) {
      if (!bucket.group || seen.has(bucket.group)) continue
      seen.set(bucket.group, bucket.client_name || bucket.group)
    }
    return [...seen.entries()].sort((a, b) => a[1].localeCompare(b[1]))
  }, [clientsQuery.data])

  const query = useQuery({
    queryKey: [
      'pbr-sessions',
      from,
      to,
      client,
      model,
      lane,
      q,
      sort,
      order,
      pagination.pageIndex,
      pagination.pageSize,
    ],
    queryFn: () =>
      listPBRSessions({
        from: Math.floor(from / 1000),
        to: Math.floor(to / 1000),
        client,
        model,
        lane,
        q,
        sort,
        order,
        page: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      }),
    staleTime: 30_000,
  })

  const applySearch = () => {
    navigate({
      search: {
        ...search,
        page: 1,
        model: draft.model || undefined,
        lane: draft.lane || undefined,
        q: draft.q || undefined,
      },
    })
  }
  const reset = () => {
    const { start, end } = getDefaultTimeRange()
    setDraft({ model: '', lane: '', q: '' })
    navigate({
      search: {
        page: 1,
        startTime: start.getTime(),
        endTime: end.getTime(),
      },
    })
  }
  const openSession = (session: PBRSession) => {
    navigate({ search: { ...search, session: session.session_key } })
  }
  const closeSession = () => {
    const next = { ...search }
    delete next.session
    navigate({ search: next })
  }

  const sessions = query.data?.items ?? []
  const summary = query.data?.summary
  const columns = useSessionColumns({ onOpen: openSession })
  const { table } = useDataTable({
    data: sessions,
    columns,
    totalCount: query.data?.total ?? 0,
    getRowId: (row) => row.session_key,
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
        <>
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
          <LogsFilterField>
            <Select
              value={client || 'all'}
              onValueChange={(value) =>
                navigate({
                  search: {
                    ...search,
                    page: 1,
                    client:
                      (value ?? 'all') === 'all' ? undefined : String(value),
                  },
                })
              }
            >
              <SelectTrigger
                className='h-8 w-36'
                aria-label={t('Client filter')}
              >
                <SelectValue>
                  {client
                    ? (clientOptions.find(([value]) => value === client)?.[1] ??
                      client)
                    : t('All clients')}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='all'>{t('All clients')}</SelectItem>
                {clientOptions.map(([value, label]) => (
                  <SelectItem key={value} value={value}>
                    {label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </LogsFilterField>
        </>
      }
      advancedFilters={
        <>
          <LogsFilterField>
            <LogsFilterInput
              placeholder={t('Request model')}
              value={draft.model}
              onChange={(event) =>
                setDraft((current) => ({
                  ...current,
                  model: event.target.value,
                }))
              }
              onKeyDown={(event) => {
                if (event.key === 'Enter') applySearch()
              }}
            />
          </LogsFilterField>
          <LogsFilterField>
            <LogsFilterInput
              placeholder={t('Route')}
              value={draft.lane}
              onChange={(event) =>
                setDraft((current) => ({
                  ...current,
                  lane: event.target.value,
                }))
              }
              onKeyDown={(event) => {
                if (event.key === 'Enter') applySearch()
              }}
            />
          </LogsFilterField>
          <LogsFilterField>
            <LogsFilterInput
              placeholder={t('Search sessions')}
              value={draft.q}
              onChange={(event) =>
                setDraft((current) => ({ ...current, q: event.target.value }))
              }
              onKeyDown={(event) => {
                if (event.key === 'Enter') applySearch()
              }}
            />
          </LogsFilterField>
          <LogsFilterField>
            <Select
              value={sort}
              onValueChange={(value) =>
                navigate({
                  search: {
                    ...search,
                    page: 1,
                    sort: String(value ?? 'last_ts') as PBRSessionSort,
                  },
                })
              }
            >
              <SelectTrigger
                className='h-8 w-40'
                aria-label={t('Sort sessions')}
              >
                <SelectValue>{t('Sort by')}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {SESSION_SORT_OPTIONS.map((option) => (
                  <SelectItem key={option} value={option}>
                    {sortLabel(option, t)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </LogsFilterField>
          <LogsFilterField>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='h-8'
              onClick={() =>
                navigate({
                  search: {
                    ...search,
                    page: 1,
                    order: order === 'desc' ? 'asc' : 'desc',
                  },
                })
              }
            >
              {order === 'desc' ? t('Newest first') : t('Oldest first')}
            </Button>
          </LogsFilterField>
        </>
      }
      mobilePinnedFilters={null}
      mobileFilters={null}
      hasActiveFilters={Boolean(client || model || lane || q)}
      hasAdvancedActiveFilters={Boolean(model || lane || q)}
      advancedFilterCount={[model, lane, q].filter(Boolean).length}
      onSearch={applySearch}
      searchLoading={query.isFetching}
      onReset={reset}
    />
  )

  return (
    <WorkspacePage
      title={t('Sessions')}
      description={t(
        'Gateway conversations grouped by session, with context fill.'
      )}
      fixedContent
    >
      <div className='flex h-full min-h-0 flex-col gap-3'>
        <div className='flex flex-wrap items-center gap-2'>
          <WindowStat
            label={t('Sessions')}
            value={String(query.data?.total ?? 0)}
          />
          <WindowStat
            label={t('Requests')}
            value={String(summary?.requests ?? 0)}
          />
          <WindowStat
            label={t('Tokens')}
            value={formatTokens(
              (summary?.prompt_tokens ?? 0) + (summary?.completion_tokens ?? 0)
            )}
          />
          <WindowStat
            label={t('Cost')}
            value={
              (summary?.estimated_cost ?? 0) > 0
                ? formatCostYuan(summary?.estimated_cost ?? 0)
                : '—'
            }
          />
          <span className='text-muted-foreground text-xs'>
            {t('Window totals, not the current page')}
          </span>
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
            emptyTitle={t('No sessions found')}
            emptyDescription={t('Try widening the time range or filters')}
            skeletonKeyPrefix='pbr-session-skeleton'
            toolbar={toolbar}
            mobileBreakpoint={900}
          />
        </div>
      </div>
      <SessionDetailSheet
        key={selectedKey ?? 'none'}
        sessionKey={selectedKey}
        onClose={closeSession}
      />
    </WorkspacePage>
  )
}
