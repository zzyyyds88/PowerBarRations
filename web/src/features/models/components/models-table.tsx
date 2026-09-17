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
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { requireServerSuccess } from '@/lib/server-error-message'

import { getModels, searchModels } from '../api'
import { DEFAULT_PAGE_SIZE } from '../constants'
import { modelsQueryKeys } from '../lib'
import { DataTableBulkActions } from './data-table-bulk-actions'
import { useModelsColumns } from './models-columns'

const route = getRouteApi('/_authenticated/models/$section')

// 模型页 = 单一平面的模型目录（ui-spec §6.3）：四列且没有分区 Tab，
// 因此不再提供「展示策略 / 同步策略」筛选（对应列已删除）。

export function ModelsTable() {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 640px)')

  // URL state management
  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: {
      defaultPage: 1,
      defaultPageSize: isMobile ? 10 : DEFAULT_PAGE_SIZE,
    },
    globalFilter: { enabled: true, key: 'filter' },
  })

  const shouldSearch = Boolean(globalFilter?.trim())

  // Fetch models data
  // eslint-disable-next-line @tanstack/query/exhaustive-deps
  const { data, isLoading, isFetching, isError, error, refetch } = useQuery({
    queryKey: modelsQueryKeys.list({
      include_channel_models: true,
      keyword: globalFilter,
      p: pagination.pageIndex + 1,
      page_size: pagination.pageSize,
    }),
    queryFn: async () => {
      if (shouldSearch) {
        return requireServerSuccess(
          await searchModels({
            include_channel_models: true,
            keyword: globalFilter,
            p: pagination.pageIndex + 1,
            page_size: pagination.pageSize,
          })
        )
      }
      return requireServerSuccess(
        await getModels({
          include_channel_models: true,
          p: pagination.pageIndex + 1,
          page_size: pagination.pageSize,
        })
      )
    },
  })

  const models = data?.data?.items || []
  const totalCount = data?.data?.total || 0

  // Columns configuration
  const columns = useModelsColumns()

  // React Table instance
  const { table } = useDataTable({
    data: models,
    getRowId: (model) =>
      model.id > 0 ? `metadata:${model.id}` : `channel:${model.model_name}`,
    columns,
    totalCount,
    // 列集合已收敛为四列；description 与 tags 默认可见，无遗留隐藏列。
    initialColumnVisibility: {},
    columnFilters,
    pagination,
    globalFilter,
    enableRowSelection: true,
    onColumnFiltersChange,
    onPaginationChange,
    onGlobalFilterChange,
    manualPagination: true,
    manualFiltering: true,
    ensurePageInRange,
  })

  if (isError || data?.success === false) {
    return (
      <ErrorState
        description={error?.message ?? data?.message}
        onRetry={() => void refetch()}
      />
    )
  }

  return (
    <DataTablePage
      showMobileBulkActions
      mobileProps={{ enableRowSelection: true }}
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No Models Found')}
      emptyDescription={
        shouldSearch
          ? t('Try adjusting your search')
          : t('No models available. Create your first model to get started.')
      }
      skeletonKeyPrefix='model-skeleton'
      applyHeaderSize
      pinnedColumns={[
        { columnId: 'model_name', side: 'left' },
        { columnId: 'actions', side: 'right' },
      ]}
      toolbarProps={{
        searchPlaceholder: t('Filter by model name...'),
        searchDebounceMs: 500,
      }}
      bulkActions={<DataTableBulkActions table={table} />}
    />
  )
}
