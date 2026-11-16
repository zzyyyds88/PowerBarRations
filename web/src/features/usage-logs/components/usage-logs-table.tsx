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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { Fragment, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePage,
  DataTableRow,
  useDataTable,
} from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { TableRow, TableCell } from '@/components/ui/table'
import { useMediaQuery } from '@/hooks'
import { usePageUrlState } from '@/hooks/use-page-url-state'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { cn } from '@/lib/utils'

import {
  DEFAULT_LOGS_DATA,
  LOG_TYPE_ALL_VALUE,
  LOG_TYPE_ENUM,
  type LogsViewMode,
} from '../constants'
import type { UsageLog } from '../data/schema'
import { useLogsTailPolling } from '../hooks/use-logs-tail'
import { useColumnsByCategory } from '../lib/columns'
import { parseLogOther } from '../lib/format'
import { fetchLogsByCategory } from '../lib/utils'
import type { LogCategory } from '../types'
import { CommonLogsFilterBar } from './common-logs-filter-bar'
import { PBRLogCardList } from './pbr-log-card-list'
import { PBRLogDetailsContent } from './pbr-log-details'
import { PBRLogsFilterBar } from './pbr-logs-filter-bar'
import { UsageLogsMobileList } from './usage-logs-mobile-card'
import {
  useLogsViewScope,
  useUsageLogsContext,
  type LogsViewAccess,
} from './usage-logs-provider'

const logTypeRowTint: Record<number, string> = {
  [LOG_TYPE_ENUM.ERROR]: 'bg-rose-50/40 dark:bg-rose-950/20',
  [LOG_TYPE_ENUM.REFUND]: 'bg-blue-50/30 dark:bg-blue-950/15',
}

// Warning tint for logs where a quota conversion saturated (admin-only marker).
// Takes precedence over the per-type tint since it flags a billing anomaly.
const quotaSaturationRowTint = 'bg-amber-50/60 dark:bg-amber-950/25'

function getColumnVisibilityStorageKey(
  logCategory: LogCategory,
  viewAccess: LogsViewAccess
): string {
  return `usage-logs:${logCategory}:${viewAccess}:column-visibility`
}

const PBR_LOG_VIEW_STORAGE_KEY = 'usage-logs:pbr:view-mode'

function readPBRLogViewMode(): LogsViewMode {
  try {
    const stored = window.localStorage.getItem(PBR_LOG_VIEW_STORAGE_KEY)
    return stored === 'cards' ? 'cards' : 'table'
  } catch {
    return 'table'
  }
}

function deserializeLogTypeFilter(value: unknown): unknown[] {
  let values: unknown[] = []
  if (Array.isArray(value)) {
    values = value
  } else if (value) {
    values = [value]
  }
  return values.filter((item) => String(item) !== LOG_TYPE_ALL_VALUE)
}

interface UsageLogsTableProps {
  logCategory: LogCategory
}

export function UsageLogsTable({ logCategory }: UsageLogsTableProps) {
  const { t } = useTranslation()
  const { isAdminView: isAdmin, viewAccess } = useLogsViewScope()
  const { tailIntervalMs } = useUsageLogsContext()
  const isMobile = useMediaQuery('(max-width: 1024px)')
  const [viewMode, setViewMode] = useState<LogsViewMode>(readPBRLogViewMode)
  const handleViewModeChange = (mode: LogsViewMode) => {
    setViewMode(mode)
    try {
      window.localStorage.setItem(PBR_LOG_VIEW_STORAGE_KEY, mode)
    } catch {
      /* localStorage can be unavailable in private modes; keep in-memory. */
    }
  }
  const { search, navigate } = usePageUrlState()
  const queryClient = useQueryClient()
  const searchParams = search

  // Stable holder so the tail hook can trigger the query's refetch without
  // coupling the hook to the query instance.
  const tailRefetchRef = useRef<() => void>(() => undefined)
  const { isDocumentVisible, refetchInterval: tailRefetchInterval } =
    useLogsTailPolling({
      intervalMs: tailIntervalMs,
      onResume: () => tailRefetchRef.current(),
    })

  const clearFilters = () => {
    // Removing the filter keys (including the explicit range) lets the list
    // fall back to its default range; a draft in the filter bar is synced from
    // the URL by that component.
    navigate({ search: { page: 1 } })
    void queryClient.invalidateQueries({ queryKey: ['logs'] })
  }
  const hasActiveFilters =
    Boolean(
      searchParams.lane ||
      searchParams.channel ||
      searchParams.key ||
      searchParams.model ||
      searchParams.client ||
      searchParams.session ||
      searchParams.success
    ) ||
    searchParams.startTime !== undefined ||
    searchParams.endTime !== undefined
  // The filter bar keeps a local draft and only reads the URL on mount. Keying
  // it by the applied filter values remounts it when the empty-state action
  // clears the URL, without resetting the draft on pagination changes.
  const filterBarKey = [
    searchParams.lane,
    searchParams.channel,
    searchParams.key,
    searchParams.model,
    searchParams.client,
    searchParams.session,
    searchParams.success,
    searchParams.startTime,
    searchParams.endTime,
  ]
    .map((value) => String(value ?? ''))
    .join('\u001f')

  const {
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search,
    navigate,
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 20 : 100 },
    globalFilter: { enabled: false },
    columnFilters:
      logCategory === 'pbr'
        ? [
            {
              columnId: 'model_name',
              searchKey: 'model',
              type: 'string' as const,
            },
            {
              columnId: 'token_name',
              searchKey: 'key',
              type: 'string' as const,
            },
            { columnId: 'lane', searchKey: 'lane', type: 'string' as const },
            {
              columnId: 'channel',
              searchKey: 'channel',
              type: 'string' as const,
            },
            {
              columnId: 'success',
              searchKey: 'success',
              type: 'string' as const,
            },
          ]
        : [
            {
              columnId: 'created_at',
              searchKey: 'type',
              type: 'array' as const,
              deserialize: deserializeLogTypeFilter,
            },
            {
              columnId: 'model_name',
              searchKey: 'model',
              type: 'string' as const,
            },
            {
              columnId: 'token_name',
              searchKey: 'token',
              type: 'string' as const,
            },
            { columnId: 'group', searchKey: 'group', type: 'string' as const },
            ...(isAdmin
              ? [
                  {
                    columnId: 'channel',
                    searchKey: 'channel',
                    type: 'string' as const,
                  },
                  {
                    columnId: 'username',
                    searchKey: 'username',
                    type: 'string' as const,
                  },
                ]
              : []),
          ],
  })

  const { data, isLoading, isFetching, isError, error, refetch } = useQuery({
    queryKey: [
      'logs',
      logCategory,
      viewAccess,
      pagination.pageIndex + 1,
      pagination.pageSize,
      columnFilters,
      searchParams,
      t,
    ],
    queryFn: async () => {
      // 基座面列表成功即裸 {items,total,...}；失败由 axios 拒绝。
      const result = await fetchLogsByCategory({
        logCategory,
        isAdmin,
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        searchParams,
        columnFilters,
      })

      return result || DEFAULT_LOGS_DATA
    },
    placeholderData: (previousData, previousQuery) => {
      if (
        previousQuery?.queryKey[1] === logCategory &&
        previousQuery.queryKey[2] === viewAccess
      ) {
        return previousData
      }
      return undefined
    },
    // Tail only runs while the tab is visible. Stopping on a failed query
    // avoids hammering the API (and repeating the global error toast) until
    // the user retries; a successful retry resumes the selected cadence.
    refetchInterval: (query) =>
      query.state.status === 'error' ? false : tailRefetchInterval,
    refetchIntervalInBackground: true,
  })

  useEffect(() => {
    tailRefetchRef.current = () => {
      void refetch()
    }
  }, [refetch])

  const logs = data?.items || []
  const columns = useColumnsByCategory(logCategory, isAdmin)
  const isLoadingData = isLoading || (isFetching && !data)

  const { table } = useDataTable({
    data: logs as Record<string, unknown>[],
    columns: columns as ColumnDef<Record<string, unknown>>[],
    // Request ids keep an expanded row expanded across tail refetches; index
    // based ids would attach the expansion to whichever row lands there next.
    getRowId: (log) => String((log as unknown as UsageLog).id),
    columnFilters,
    columnVisibilityStorageKey: getColumnVisibilityStorageKey(
      logCategory,
      viewAccess
    ),
    pagination,
    enableRowSelection: false,
    onPaginationChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  const isCommon = logCategory === 'common' || logCategory === 'pbr'
  // Background tail refetches must not dim and disable the table every tick:
  // the user may be reading an expanded row while new data arrives.
  const tailActive = tailIntervalMs > 0 && isDocumentVisible
  const emptyAction = hasActiveFilters ? (
    <Button variant='outline' size='sm' onClick={clearFilters}>
      {t('Clear filters')}
    </Button>
  ) : undefined
  const emptyTitle = t('No Logs Found')
  const emptyDescription = t(
    'No usage logs available. Logs will appear here once API calls are made.'
  )
  // Desktop card view is a PBR-only alternate presentation; the table stays the
  // single source of filtering/pagination/tail state.
  const showCardView =
    logCategory === 'pbr' && !isMobile && viewMode === 'cards'
  const desktopView = showCardView ? (
    <PBRLogCardList
      table={table}
      isLoading={isLoadingData}
      emptyTitle={emptyTitle}
      emptyDescription={emptyDescription}
      emptyAction={emptyAction}
    />
  ) : undefined

  return (
    <DataTablePage
      table={table}
      mobileBreakpoint={1024}
      compactPagination={isMobile && isCommon}
      columns={columns as ColumnDef<Record<string, unknown>>[]}
      isLoading={isLoadingData}
      isFetching={isFetching && !tailActive}
      isError={isError}
      error={error}
      errorTitle={t('Failed to load request logs')}
      onRetry={() => void refetch()}
      emptyTitle={emptyTitle}
      emptyDescription={emptyDescription}
      emptyAction={emptyAction}
      desktop={desktopView}
      skeletonKeyPrefix='usage-log-skeleton'
      applyHeaderSize
      tableClassName={cn(
        '[&_[data-slot=table]]:text-[12px] [&_[data-slot=table]_td]:text-[12px] [&_[data-slot=table]_td_*]:text-[12px] [&_[data-slot=table]_th]:text-[12px] [&_[data-slot=table]_th_*]:text-[12px] [&_[data-slot=table]_td]:py-1 [&_[data-slot=table]_th]:py-1.5'
      )}
      mobile={
        <UsageLogsMobileList
          table={table}
          isLoading={isLoadingData}
          logCategory={logCategory}
          emptyAction={emptyAction}
        />
      }
      toolbar={
        logCategory === 'pbr' ? (
          <PBRLogsFilterBar
            key={filterBarKey}
            table={table}
            viewMode={viewMode}
            onViewModeChange={handleViewModeChange}
          />
        ) : (
          <CommonLogsFilterBar table={table} />
        )
      }
      renderRow={(row) => {
        const log = row.original
        const logType = log.type as number | undefined
        let tintClass =
          isCommon && logType != null ? (logTypeRowTint[logType] ?? '') : ''
        if (isCommon && isAdmin) {
          const other = parseLogOther((log.other as string) ?? '')
          if (other?.admin_info?.quota_saturation) {
            tintClass = quotaSaturationRowTint
          }
        }

        return (
          <Fragment key={row.id}>
            <DataTableRow
              row={row}
              className={cn('transition-colors', tintClass)}
              getColumnClassName={() => (isCommon ? 'py-1.5' : 'py-3.5')}
            />
            {row.getIsExpanded() && (
              <TableRow className={cn('bg-muted/40 border-b', tintClass)}>
                <TableCell colSpan={columns.length} className='p-3'>
                  <PBRLogDetailsContent log={log as unknown as UsageLog} />
                </TableCell>
              </TableRow>
            )}
          </Fragment>
        )
      }}
    />
  )
}
