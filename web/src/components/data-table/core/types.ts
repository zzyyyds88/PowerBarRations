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
import type { Row, Table as TanstackTable } from '@tanstack/react-table'
import type * as React from 'react'

export type DataTableColumnClassName = (
  columnId: string,
  kind: 'header' | 'cell'
) => string | undefined

export type DataTablePinnedColumn = {
  columnId: string
  side: 'left' | 'right'
  className?: string
  headerClassName?: string
  cellClassName?: string
}

export type DataTableRenderRowHelpers = {
  getCellClassName: (columnId: string, className?: string) => string | undefined
}

export type DataTableViewProps<TData> = {
  table: TanstackTable<TData>
  isLoading?: boolean
  rows?: Row<TData>[]
  emptyTitle?: string
  emptyDescription?: string
  emptyIcon?: React.ReactNode
  emptyAction?: React.ReactNode
  emptyContent?: React.ReactNode
  emptyCellClassName?: string
  skeletonKeyPrefix?: string
  skeletonRowHeight?: string
  renderRow?: (
    row: Row<TData>,
    helpers: DataTableRenderRowHelpers
  ) => React.ReactNode
  getRowClassName?: (row: Row<TData>) => string | undefined
  getColumnClassName?: DataTableColumnClassName
  pinnedColumns?: DataTablePinnedColumn[]
  applyHeaderSize?: boolean
  /**
   * Let cell content wrap instead of forcing `whitespace-nowrap`, and drop the
   * `min-width` floor built from the column `size` values. Use it on tables
   * whose column widths would otherwise force a horizontal scroll on narrower
   * viewports. Off by default so existing pages keep single-line rows.
   */
  wrap?: boolean
  tableClassName?: string
  tableHeaderClassName?: string
  tableHeaderRowClassName?: string
  tableBodyClassName?: string
  tableBodyRowClassName?: string
  splitHeader?: boolean
  splitHeaderScrollClassName?: string
  bodyContainerClassName?: string
  containerClassName?: string
  containerProps?: Omit<React.ComponentProps<'div'>, 'className' | 'children'>
  tableContainerClassName?: string
  colgroup?: React.ReactNode
}
