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
 *     `stickyHeader` tables, the page content area otherwise). They keep the
 *     opaque `--table-header` band as the no-blur fallback and swap to the
 *     frosted `--surface-glass` band (plus `backdrop-blur`) where supported,
 *     so rows never bleed through.
 *   - Rows highlight on hover with a blue wash that fades out to the right
 *     (`--surface-row-hover`).
 *   - Cells default to `whitespace-nowrap`, which is a *hard floor* under the
 *     table width: `nowrap` holds each cell at its max-content width, so a wide
 *     table can never be narrowed by its container and answers a smaller
 *     viewport with a horizontal scrollbar instead of tighter columns. `wrap`
 *     opts a table out of that floor.
 *
 * `data-*` contract (rules live in `styles/index.css`):
 *   - `data-wrap="true"` on the `<table>`, or on an individual `th`/`td`, lets
 *     that cell's content break onto several lines; `data-wrap="false"` on a
 *     single cell opts it back into `nowrap` inside a wrapping table.
 *   - `data-scroll-port="true"` marks the element that scrolls the table, and
 *     turns it into a `scroll-state` container so the trailing edge fade can
 *     exist exactly while there is more table to the right.
 *
 * `wrap` is the fix for the floor described above, and it is what these pages
 * actually use: a table that fits never scrolls, so a `sticky right-0` column
 * has nothing left to cover. The trailing edge fade covers what remains — a
 * table that still overflows (a resized `/channels`, or any future wide table)
 * at least says so, instead of leaving the operator to discover the scroll by
 * accident.
 */

/** Element that scrolls a wide table horizontally. */
const SCROLL_PORT_ATTR = 'data-scroll-port'
/** Inner height of the scroll port; sizes the trailing edge fade. */
const SCROLL_PORT_HEIGHT_VAR = '--table-scroll-port-height'
/** Total width of the trailing sticky columns, when there are any. */
const PINNED_GUTTER_VAR = '--table-pinned-gutter'

/**
 * Marks `port` as the element scrolling a table and keeps two measurements in
 * sync as custom properties on it, so {@link TableScrollHint} can size and
 * place itself against the port it is pinned to:
 *
 * - `--table-scroll-port-height`: the port's inner height.
 * - `--table-pinned-gutter`: the total width of the trailing sticky columns,
 *   so the fade can stop at the seam where the pinned column starts instead of
 *   washing over that column's own content.
 *
 * Both are written imperatively, and only when the value changed, so keeping
 * them current never re-renders the table.
 *
 * Pass the returned callback as the scroll port's `ref`.
 */
export function useTableScrollPort() {
  return React.useCallback((port: HTMLElement | null) => {
    if (!port) return

    port.setAttribute(SCROLL_PORT_ATTR, 'true')

    const measure = () => {
      const table = port.querySelector('table[data-slot="table"]')

      let gutter = 0
      if (table) {
        for (const cell of table.querySelectorAll<HTMLElement>(
          'thead [data-slot="table-head"]'
        )) {
          const style = window.getComputedStyle(cell)
          if (style.position === 'sticky' && style.right !== 'auto') {
            gutter += cell.offsetWidth
          }
        }
      }

      const nextGutter = `${gutter}px`
      if (port.style.getPropertyValue(PINNED_GUTTER_VAR) !== nextGutter) {
        port.style.setProperty(PINNED_GUTTER_VAR, nextGutter)
      }

      const nextHeight = `${port.clientHeight}px`
      if (port.style.getPropertyValue(SCROLL_PORT_HEIGHT_VAR) !== nextHeight) {
        port.style.setProperty(SCROLL_PORT_HEIGHT_VAR, nextHeight)
      }
    }

    measure()

    if (typeof ResizeObserver === 'undefined') return

    const observer = new ResizeObserver(measure)
    /* `border-box`: the port's content box is what the fade tracks, and this
     * keeps a padding-driven content-box change from re-triggering itself. */
    observer.observe(port, { box: 'border-box' })
    const table = port.querySelector('table[data-slot="table"]')
    if (table) observer.observe(table, { box: 'border-box' })
    /* The pinned cells' widths only settle after the browser has laid the
     * table out, which is after the first measure. */
    port.addEventListener('scroll', measure, { passive: true })

    /* No teardown: a scroll port is created and discarded with the table it
     * holds, and the observer dies with the element. */
  }, [])
}

/**
 * Trailing edge fade telling the operator the table continues to the right.
 * Render inside the scroll port, before the `<table>`.
 *
 * The wrapper is a zero-height `sticky` element, which is what holds it (and
 * the bar positioned against it) at the port's trailing edge while the table
 * scrolls underneath; `w-full` puts the bar on the port's visible right edge,
 * and `--table-pinned-gutter` shifts it left to the data/pin seam when the
 * table has a trailing sticky column. Purely decorative: `aria-hidden`, never a
 * pointer target, and hidden by CSS unless the port can still scroll further
 * right.
 *
 * Its stacking is deliberate: above the ordinary cells, below the sticky
 * header (`z-10`) and the pinned columns (`z-10` / `z-30`), so the fade marks
 * the cut-off cells without washing over the opaque header band or the pinned
 * column's own content.
 */
export function TableScrollHint() {
  return (
    <div
      data-slot='table-scroll-hint'
      aria-hidden='true'
      className='pointer-events-none sticky top-0 left-0 z-[5] h-0 w-full'
    >
      <div
        data-slot='table-scroll-hint-bar'
        className='absolute top-0 right-0 w-6'
        style={{
          height: `var(${SCROLL_PORT_HEIGHT_VAR}, 0px)`,
          marginInlineEnd: `var(${PINNED_GUTTER_VAR}, 0px)`,
        }}
      />
    </div>
  )
}

function Table({
  className,
  withContainer = true,
  containerClassName,
  stickyHeader = false,
  wrap = false,
  ...props
}: React.ComponentProps<'table'> & {
  withContainer?: boolean
  /** Extra classes for the scroll container (e.g. the card's max height). */
  containerClassName?: string
  /** Scroll vertically inside the container with a sticky header band. */
  stickyHeader?: boolean
  /**
   * Let cells wrap instead of forcing `whitespace-nowrap`. Off by default: the
   * data-dense pages are built around single-line rows, so tables opt in one at
   * a time — and only where a horizontal scroll would otherwise be forced.
   */
  wrap?: boolean
}) {
  const portRef = useTableScrollPort()

  const table = (
    <table
      data-slot='table'
      data-wrap={wrap ? 'true' : undefined}
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
      ref={portRef}
      data-slot='table-container'
      data-sticky-header={stickyHeader ? 'true' : undefined}
      className={cn(
        'group/table relative w-full',
        stickyHeader ? 'overflow-auto' : 'overflow-x-auto overflow-y-hidden',
        containerClassName
      )}
    >
      <TableScrollHint />
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
        'group-data-[sticky-header=true]/table:sticky group-data-[sticky-header=true]/table:top-0 group-data-[sticky-header=true]/table:z-10 group-data-[sticky-header=true]/table:bg-(--table-header) group-data-[sticky-header=true]/table:supports-backdrop-filter:bg-(--surface-glass) group-data-[sticky-header=true]/table:supports-backdrop-filter:backdrop-blur-md',
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
        'group data-[state=selected]:bg-muted border-b transition-colors hover:[background-image:var(--surface-row-hover)] has-aria-expanded:[background-color:var(--table-row-hover)]',
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
