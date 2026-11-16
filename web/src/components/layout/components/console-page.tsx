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
import { useState, type ReactNode } from 'react'

import { cn } from '@/lib/utils'

import { Main } from './main'
import { PageFooterProvider } from './page-footer'

export type ConsolePageProps = {
  title: ReactNode
  /** One line, ≤ ~75 characters. Keep it actionable, not decorative. */
  description?: ReactNode
  actions?: ReactNode
  tabs?: ReactNode
  breadcrumb?: ReactNode
  fixedContent?: boolean
  stackActionsOnMobile?: boolean
  children: ReactNode
}

/**
 * The single console page shell (ui-spec §5/§11).
 *
 * Every console destination renders through this component: title, one-line
 * description, one actions cluster, an optional workspace tab strip, and a
 * single scrolling content region. Do not add another implementation.
 *
 * Layout tokens come from `theme.css` (`px-page`, `py-…`, `text-title`, …)
 * so the shell stays the only place that decides page rhythm.
 *
 * PageFooterProvider and the footer portal are mandatory: DataTablePage sends
 * pagination through the portal. Dropping them silently removes pagination.
 */
export function ConsolePage(props: ConsolePageProps) {
  const [footerContainer, setFooterContainer] = useState<HTMLDivElement | null>(
    null
  )

  return (
    <PageFooterProvider container={footerContainer}>
      <Main>
        <div className='border-border bg-card px-page max-sm:px-page-mobile shrink-0 border-b [background-image:linear-gradient(180deg,color-mix(in_oklab,var(--foreground)_2%,transparent),transparent)] py-3'>
          {props.breadcrumb != null && (
            <div className='mb-1.5'>{props.breadcrumb}</div>
          )}
          <div className='flex flex-wrap items-start justify-between gap-x-4 gap-y-2 max-sm:flex-col'>
            <div
              className={cn(
                'min-w-0 flex-1',
                (props.stackActionsOnMobile ?? true) && 'max-sm:basis-full'
              )}
            >
              <h2 className='text-title sm:text-subhead font-serif font-semibold tracking-[-0.02em] break-words'>
                {props.title}
              </h2>
              {props.description != null && (
                <p className='text-muted-foreground text-caption mt-0.5 max-w-[75ch] max-sm:line-clamp-2 max-sm:whitespace-normal sm:truncate'>
                  {props.description}
                </p>
              )}
            </div>
            {props.actions != null && (
              <div className='flex shrink-0 flex-wrap items-center justify-end gap-1.5 max-sm:w-full max-sm:justify-start'>
                {props.actions}
              </div>
            )}
          </div>
          {props.tabs != null && (
            <div className='mt-2.5 min-w-0'>{props.tabs}</div>
          )}
        </div>

        <div
          className={cn(
            'min-h-0 flex-1 px-page py-page max-sm:px-page-mobile',
            props.fixedContent ? 'overflow-hidden' : 'overflow-auto'
          )}
        >
          {props.children}
        </div>

        <div
          ref={setFooterContainer}
          className='bg-background px-page max-sm:px-page-mobile shrink-0 border-t py-2 empty:hidden'
        />
      </Main>
    </PageFooterProvider>
  )
}
