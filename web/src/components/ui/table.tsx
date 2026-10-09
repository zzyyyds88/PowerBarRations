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
'use client'

import * as React from 'react'

import { cn } from '@/lib/utils'

/*
 * Data-dense table primitives:
 *   - 36px body rows / 32px header cells, driven by the `--density-*` tokens.
 *   - Header cells are sticky against whatever scrolls them (the card body on
 *     `stickyHeader` tables, the page content area otherwise) and carry the
 *     opaque `--table-header` band so rows never bleed through.
 *   - Rows highlight on hover via `--table-row-hover`.
 */
function Table({
  className,
  withContainer = true,
  containerClassName,
  stickyHeader = false,
  ...props
}: React.ComponentProps<'table'> & {
  withContainer?: boolean
  /** Extra classes for the scroll container (e.g. the card's max height). */
  containerClassName?: string
  /** Scroll vertically inside the container with a sticky header band. */
  stickyHeader?: boolean
}) {
  const table = (
    <table
      data-slot='table'
      className={cn(
        'w-full caption-bottom text-sm tabular-nums [font-family:var(--font-body)] [&_td]:text-sm [&_td]:font-medium [&_th]:text-xs [&_th]:font-medium [&_:is(th,td)_*]:[font-family:inherit] [&_:is(th,td)_*]:[font-size:inherit] [&_:is(th,td)_*]:[font-weight:inherit]',
        '[&_[data-table-text=secondary]]:text-xs [&_[data-table-text=secondary]]:font-normal',
        className
      )}
      {...props}
    />
  )

  if (!withContainer) return table

  return (
    <div
      data-slot='table-container'
      data-sticky-header={stickyHeader ? 'true' : undefined}
      className={cn(
        'group/table relative w-full',
        stickyHeader ? 'overflow-auto' : 'overflow-x-auto overflow-y-hidden',
        containerClassName
      )}
    >
      {table}
    </div>
  )
}

function TableHeader({ className, ...props }: React.ComponentProps<'thead'>) {
  return (
    <thead
      data-slot='table-header'
      className={cn(
        '[&_tr]:border-b',
        /* Belt and braces with the sticky `th` rule: engines differ on whether
         * sticky applies to the table section or only to the cells once
         * `border-collapse: collapse` is in play. */
        'group-data-[sticky-header=true]/table:sticky group-data-[sticky-header=true]/table:top-0 group-data-[sticky-header=true]/table:z-10 group-data-[sticky-header=true]/table:bg-(--table-header)',
        className
      )}
      {...props}
    />
  )
}

function TableBody({ className, ...props }: React.ComponentProps<'tbody'>) {
  return (
    <tbody
      data-slot='table-body'
      className={cn(
        '[&>tr]:h-(--density-table-row-height) [&_tr:last-child]:border-0',
        className
      )}
      {...props}
    />
  )
}

function TableFooter({ className, ...props }: React.ComponentProps<'tfoot'>) {
  return (
    <tfoot
      data-slot='table-footer'
      className={cn(
        'bg-muted/50 border-t font-medium [&>tr]:last:border-b-0',
        className
      )}
      {...props}
    />
  )
}

function TableRow({ className, ...props }: React.ComponentProps<'tr'>) {
  return (
    <tr
      data-slot='table-row'
      className={cn(
        'group data-[state=selected]:bg-muted border-b transition-colors hover:[background-color:var(--table-row-hover)] has-aria-expanded:[background-color:var(--table-row-hover)]',
        className
      )}
      {...props}
    />
  )
}

function TableHead({ className, ...props }: React.ComponentProps<'th'>) {
  return (
    <th
      data-slot='table-head'
      className={cn(
        'text-muted-foreground h-(--density-table-head-height) px-2 text-left align-middle whitespace-nowrap [&:has([role=checkbox])]:pr-0',
        'group-data-[sticky-header=true]/table:sticky group-data-[sticky-header=true]/table:top-0 group-data-[sticky-header=true]/table:z-10 group-data-[sticky-header=true]/table:bg-(--table-header)',
        className
      )}
      {...props}
    />
  )
}

function TableCell({ className, ...props }: React.ComponentProps<'td'>) {
  return (
    <td
      data-slot='table-cell'
      className={cn(
        'px-2 py-1 align-middle whitespace-nowrap [&:has([role=checkbox])]:pr-0',
        className
      )}
      {...props}
    />
  )
}

function TableCaption({
  className,
  ...props
}: React.ComponentProps<'caption'>) {
  return (
    <caption
      data-slot='table-caption'
      className={cn('text-muted-foreground mt-3 text-xs', className)}
      {...props}
    />
  )
}

export {
  Table,
  TableHeader,
  TableBody,
  TableFooter,
  TableHead,
  TableRow,
  TableCell,
  TableCaption,
}
