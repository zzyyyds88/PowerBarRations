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
import { ChevronDown, SlidersHorizontal } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

type DataTableMobileFilterPanelProps = {
  children: ReactNode
  actions: ReactNode
  /**
   * `drawer` moves the complete filter form behind one "Filters" button.
   * Used by tables whose inline mobile fields would otherwise push the table
   * below the fold. The default keeps the legacy compact inline row for
   * callers that already own a drawer (e.g. request logs).
   */
  drawer?: boolean
  activeFilterCount?: number
  onReset?: () => void
  compact?: boolean
  className?: string
}

export function DataTableMobileFilterPanel(
  props: DataTableMobileFilterPanelProps
) {
  return props.drawer ? (
    <MobileFilterDrawer {...props} />
  ) : (
    <MobileFilterRow {...props} />
  )
}

function MobileFilterRow(props: DataTableMobileFilterPanelProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(true)

  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className={cn(
        'bg-card/50 flex min-w-0 flex-col rounded-lg border p-2.5',
        open && (props.compact ? 'gap-2.5' : 'gap-2'),
        props.className
      )}
    >
      <CollapsibleContent>{props.children}</CollapsibleContent>
      <div
        className={cn(
          props.compact
            ? 'grid min-w-0 grid-cols-[auto_minmax(0,1fr)] items-start gap-2'
            : 'flex items-center justify-end gap-1.5'
        )}
      >
        <CollapsibleTrigger
          aria-label={open ? t('Collapse') : t('Expand')}
          render={
            <Button
              variant='ghost'
              size='icon'
              className={cn(
                'text-muted-foreground hover:text-foreground',
                props.compact ? 'size-9' : 'mr-auto size-7'
              )}
            />
          }
        >
          <ChevronDown
            aria-hidden='true'
            className={cn(
              'transition-transform duration-200',
              props.compact ? 'size-4' : 'size-3.5',
              open && 'rotate-180'
            )}
          />
        </CollapsibleTrigger>
        <div
          role='group'
          aria-label={t('Actions')}
          className={cn(
            'flex items-center justify-end gap-1.5',
            props.compact &&
              'min-w-0 flex-wrap [&>button]:h-auto [&>button]:min-h-9 [&>button]:max-w-full [&>button]:[overflow-wrap:anywhere] [&>button]:whitespace-normal'
          )}
        >
          {props.actions}
        </div>
      </div>
    </Collapsible>
  )
}

/**
 * Mobile drawer variant.
 *
 * The primary object on a phone is the table, so filters move into a right
 * sheet behind one "Filters" button; quick actions stay on the single toolbar
 * row. Filter values apply as they are edited; the footer only resets or
 * dismisses.
 */
function MobileFilterDrawer(props: DataTableMobileFilterPanelProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const activeCount = props.activeFilterCount ?? 0

  return (
    <>
      <div className={cn('flex min-w-0 items-center gap-2', props.className)}>
        <Button
          type='button'
          variant='outline'
          size='sm'
          aria-label={t('Filters')}
          className='shrink-0 gap-1.5'
          onClick={() => setOpen(true)}
        >
          <SlidersHorizontal className='size-3.5' />
          {t('Filters')}
          {activeCount > 0 && (
            <span className='bg-primary/10 text-primary rounded-full px-1.5 text-[10px] font-semibold tabular-nums'>
              {activeCount}
            </span>
          )}
        </Button>
        <div
          role='group'
          aria-label={t('Actions')}
          className='ms-auto flex min-w-0 items-center justify-end gap-1.5'
        >
          {props.actions}
        </div>
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent variant='sheet' className='w-full gap-0 p-0'>
          <DialogHeader className='border-border border-b px-4 py-3'>
            <DialogTitle>{t('Filters')}</DialogTitle>
          </DialogHeader>
          <div className='min-h-0 flex-1 overflow-y-auto px-4 py-3 [&_button]:min-h-9 [&_input]:h-9'>
            {props.children}
          </div>
          <div className='border-border flex shrink-0 gap-2 border-t px-4 py-3'>
            <Button
              type='button'
              variant='outline'
              className='flex-1'
              disabled={activeCount === 0}
              onClick={() => props.onReset?.()}
            >
              {t('Reset')}
            </Button>
            <DialogClose render={<Button type='button' className='flex-1' />}>
              {t('Done')}
            </DialogClose>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
