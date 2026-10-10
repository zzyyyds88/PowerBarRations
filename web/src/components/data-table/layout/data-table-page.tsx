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
import type {
  ColumnDef,
  Row,
  Table as TanstackTable,
} from '@tanstack/react-table'
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
import * as React from 'react'

import { ErrorState } from '@/components/error-state'
import { PageFooterPortal } from '@/components/layout/components/page-footer'
import { useMediaQuery } from '@/hooks'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import {
  DataTableView,
  type DataTableColumnClassName,
  type DataTablePinnedColumn,
  type DataTableRenderRowHelpers,
} from '../core/data-table-view'
import { DataTablePagination } from '../core/pagination'
import { DataTableToolbar } from '../toolbar/toolbar'
import { MobileCardList } from './mobile-card-list'

/**
 * Pass-through configuration for the default {@link DataTableToolbar}.
 * Pass `toolbar` (ReactNode) instead to fully replace the default toolbar.
 */
export type DataTablePageToolbarProps<TData> = Omit<
  React.ComponentProps<typeof DataTableToolbar<TData>>,
  'table'
>

export type DataTablePageProps<TData> = {
  /**
   * TanStack Table instance returned from `useReactTable`.
   */
  table: TanstackTable<TData>

  /**
   * Column definitions. Used for skeleton column count and empty-state colSpan.
   */
  columns: ColumnDef<TData, unknown>[]

  /**
   * Initial loading state — renders {@link TableSkeleton} or mobile skeleton.
   */
  isLoading?: boolean

  /**
   * Refetch / background loading — dims the table without removing rows.
   */
  isFetching?: boolean

  /**
   * Query failure — renders {@link ErrorState} with a retry action instead of
   * the empty state, so a failed request is never mistaken for "no data".
   */
  isError?: boolean

  /**
   * Error backing the failed query. Its safe server message, when available,
   * becomes the error-state description.
   */
  error?: unknown

  /**
   * Retry handler for the error state.
   */
  onRetry?: () => void

  /**
   * Empty-state title (used for both desktop {@link TableEmpty} and mobile fallback).
   */
  emptyTitle?: string

  /**
   * Empty-state description.
   */
  emptyDescription?: string

  /**
   * Empty-state icon override (desktop only; mobile uses default Database icon).
   */
  emptyIcon?: React.ReactNode

  /**
   * Empty-state extra content — e.g. a "Create" button below the message.
   */
  emptyAction?: React.ReactNode

  /**
   * Custom toolbar node — fully replaces the default {@link DataTableToolbar}.
   * Useful for layouts like "primary buttons + toolbar" or feature-specific filter cards.
   * If provided, `toolbarProps` is ignored.
   */
  toolbar?: React.ReactNode

  /**
   * Pass-through props for the default {@link DataTableToolbar}.
   * Ignored if `toolbar` is provided. Pass `null` to omit the toolbar entirely.
   */
  toolbarProps?: DataTablePageToolbarProps<TData> | null

  /**
   * Bulk action bar — typically a wrapped {@link DataTableBulkActions} component.
   * Rendered only on desktop (mobile selection is uncommon).
   */
  bulkActions?: React.ReactNode

  /** Allow selection actions in the mobile list when opted in. */
  showMobileBulkActions?: boolean

  /**
   * Custom mobile list node — fully replaces the default {@link MobileCardList}.
   */
  mobile?: React.ReactNode

  /**
   * Pass-through props for the default {@link MobileCardList}.
   * Ignored if `mobile` is provided.
   */
  mobileProps?: {
    enableRowSelection?: boolean
    getRowKey?: (row: Row<TData>) => string | number
    getRowClassName?: (row: Row<TData>) => string | undefined
  }

  /**
   * Disable the mobile-specific layout entirely — always renders desktop table.
   * Useful for pages where the table is read-only and short.
   */
  hideMobile?: boolean

  /**
   * Pixel viewport width at/below which the mobile card layout is shown
   * instead of the desktop table. Defaults to 640 (Tailwind `sm`); raise it
   * for pages whose wide tables overflow below a larger width
   * (e.g. usage-logs' 10-column table uses 768).
   */
  mobileBreakpoint?: number

  /**
   * Row className resolver — applied to both desktop `TableRow` and mobile card.
   * Composes with the default `data-state="selected"` styling on desktop.
   * The `ctx.isMobile` flag is provided so consumers can return the
   * appropriate variant (e.g. `DISABLED_ROW_DESKTOP` vs `DISABLED_ROW_MOBILE`)
   * without having to re-call `useMediaQuery` themselves.
   */
  getRowClassName?: (
    row: Row<TData>,
    ctx: { isMobile: boolean }
  ) => string | undefined

  /**
   * Custom desktop row renderer — replaces the default `<TableRow>`/`<TableCell>` mapping.
   * Use for expanded rows, aggregate rows, click-on-row navigation, etc.
   */
  renderRow?: (
    row: Row<TData>,
    helpers: DataTableRenderRowHelpers
  ) => React.ReactNode

  /**
   * Desktop column className resolver. Use for semantic alignment/spacing only;
   * fixed-column behavior should be configured with `pinnedColumns`.
   */
  getColumnClassName?: DataTableColumnClassName

  /**
   * Fixed desktop columns. The shared table component owns sticky position,
   * layering, shadows, and row-state backgrounds.
   */
  pinnedColumns?: DataTablePinnedColumn[]

  /**
   * Apply explicit column widths from `header.getSize()` to `<TableHead>`.
   * Enable this when your column definitions include `size` and you want it honored.
   * Off by default (TanStack Table assigns a default size of 150 to all columns
   * which would unintentionally constrain layouts that don't define sizes).
   */
  applyHeaderSize?: boolean

  /**
   * Let cell content wrap instead of forcing `whitespace-nowrap`, and drop the
   * `min-width` floor built from the column `size` values. Use it on pages whose
   * columns would otherwise force a horizontal scroll on narrower viewports.
   */
  wrap?: boolean

  /**
   * Optional skeleton key prefix for stable React keys across re-renders.
   */
  skeletonKeyPrefix?: string

  /**
   * Whether to render pagination. Defaults to `true`.
   */
  showPagination?: boolean

  /** Minimal previous/next pagination for narrow feature layouts. */
  compactPagination?: boolean

  /**
   * Render pagination via `PageFooterPortal` (sticks to page footer).
   * Defaults to `true`. Set `false` to render inline below the table.
   */
  paginationInFooter?: boolean

  /**
   * Extra content rendered between the table/mobile list and the pagination.
   * E.g. summary stats, helper text.
   */
  afterTable?: React.ReactNode

  /**
   * Outer wrapper className (applied to the toolbar+table column).
   */
  className?: string

  /**
   * Make the desktop table consume the available page height and scroll inside
   * the table body while keeping the header fixed. Defaults to `true`.
   */
  fixedHeight?: boolean

  /**
   * Desktop table container className (the bordered scroll wrapper).
   */
  tableClassName?: string

  /**
   * Desktop `<TableHeader>` className override.
   * Use for header color/spacing overrides. Fixed-height pages keep the header
   * outside the scrollable body automatically.
   */
  tableHeaderClassName?: string
}

/**
 * Unified table page wrapper. Encapsulates the canonical structure used across
 * all list pages: toolbar → desktop table / mobile list → pagination, plus
 * loading/empty states and an opt-in bulk action bar.
 *
 * Most pages should be expressible as:
 * ```tsx
 * <DataTablePage
 *   table={table}
 *   columns={columns}
 *   isLoading={isLoading}
 *   isFetching={isFetching}
 *   emptyTitle={t('No X Found')}
 *   toolbarProps={{ searchPlaceholder: t('Filter...'), filters }}
 *   bulkActions={<MyBulkActions table={table} />}
 * />
 * ```
 *
 * For complex layouts (custom mobile, expanded rows, custom toolbar), use the
 * `toolbar` / `mobile` / `renderRow` slots instead of the `*Props` variants.
 */
export function DataTablePage<TData>(props: DataTablePageProps<TData>) {
  const isMobile = useMediaQuery(
    `(max-width: ${props.mobileBreakpoint ?? 640}px)`
  )
  const showMobile = isMobile && !props.hideMobile

  // A failed request must not masquerade as the "No ... Found" empty state.
  if (props.isError) {
    return (
      <ErrorState
        description={
          props.error != null ? getServerErrorMessage(props.error) : undefined
        }
        onRetry={props.onRetry}
        className={cn('min-h-[300px]', props.className)}
      />
    )
  }

  const toolbarNode = renderToolbar(props, undefined)
  const mobileNode = renderMobile(props, showMobile)
  const desktopNode = renderDesktop(props, showMobile)
  const paginationNode = renderPagination(props)

  return (
    <>
      <div
        className={cn(
          props.fixedHeight !== false
            ? 'flex h-full min-h-0 flex-col gap-2'
            : 'space-y-2',
          props.className
        )}
      >
        {toolbarNode}
        {mobileNode}
        {desktopNode}
        {props.afterTable}
      </div>

      {/* Bulk actions are typically a fixed-position toolbar; let the consumer
          handle its own visibility, we just gate it to non-mobile. */}
      {(!showMobile || props.showMobileBulkActions) && props.bulkActions}

      {paginationNode}
    </>
  )
}

function renderToolbar<TData>(
  props: DataTablePageProps<TData>,
  viewToggle: React.ReactNode
): React.ReactNode {
  if (props.toolbar !== undefined) {
    // Fully custom toolbar: the consumer owns layout, including any toggle.
    return props.toolbar
  }
  if (props.toolbarProps === null) {
    return null
  }
  if (props.toolbarProps) {
    return (
      <DataTableToolbar
        table={props.table}
        {...props.toolbarProps}
        viewToggle={props.toolbarProps.viewToggle ?? viewToggle}
      />
    )
  }
  return null
}

function renderPagination<TData>(
  props: DataTablePageProps<TData>
): React.ReactNode {
  if (props.showPagination === false) {
    return null
  }

  const pagination = (
    <DataTablePagination
      table={props.table}
      compact={props.compactPagination}
    />
  )

  return props.paginationInFooter !== false ? (
    <PageFooterPortal>{pagination}</PageFooterPortal>
  ) : (
    <div className='pt-2'>{pagination}</div>
  )
}

function renderMobile<TData>(
  props: DataTablePageProps<TData>,
  showMobile: boolean
): React.ReactNode {
  if (!showMobile) {
    return null
  }

  const ownGetRowClassName = props.getRowClassName
  const mobileGetRowClassName =
    props.mobileProps?.getRowClassName ??
    (ownGetRowClassName
      ? (row: Row<TData>) => ownGetRowClassName(row, { isMobile: true })
      : undefined)

  let mobileContent = props.mobile
  if (mobileContent === undefined) {
    // 窄屏一律用 MobileCardList（每行一张卡的列表兜底）。桌面「卡片视图」已随
    // 2026-10-10 的收敛移除；移动端这条腿与它无关，必须保留。
    mobileContent = (
      <MobileCardList
        enableRowSelection={props.mobileProps?.enableRowSelection}
        table={props.table}
        isLoading={props.isLoading}
        emptyTitle={props.emptyTitle}
        emptyDescription={props.emptyDescription}
        getRowKey={props.mobileProps?.getRowKey}
        getRowClassName={mobileGetRowClassName}
      />
    )
  }

  return <div className='min-h-0 flex-1 overflow-y-auto'>{mobileContent}</div>
}

function renderDesktop<TData>(
  props: DataTablePageProps<TData>,
  showMobile: boolean
): React.ReactNode {
  if (showMobile) {
    return null
  }

  const isFetchingOnly = props.isFetching && !props.isLoading
  const fixedHeight = props.fixedHeight !== false

  return (
    <DataTableView
      table={props.table}
      isLoading={props.isLoading}
      emptyTitle={props.emptyTitle}
      emptyDescription={props.emptyDescription}
      emptyIcon={props.emptyIcon}
      emptyAction={props.emptyAction}
      skeletonKeyPrefix={props.skeletonKeyPrefix}
      renderRow={props.renderRow}
      applyHeaderSize={props.applyHeaderSize}
      wrap={props.wrap}
      splitHeader={fixedHeight}
      tableContainerClassName={fixedHeight ? 'h-full min-h-0' : undefined}
      tableHeaderClassName={cn(
        fixedHeight && '[background-color:var(--table-header)]',
        props.tableHeaderClassName
      )}
      getColumnClassName={props.getColumnClassName}
      pinnedColumns={props.pinnedColumns}
      containerClassName={cn(
        fixedHeight && 'min-h-0 flex-1',
        'transition-opacity duration-150',
        isFetchingOnly && 'pointer-events-none opacity-60',
        props.tableClassName
      )}
      getRowClassName={(row) =>
        props.getRowClassName?.(row, { isMobile: false })
      }
    />
  )
}
