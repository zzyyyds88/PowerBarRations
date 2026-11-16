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
import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import { FadeIn } from '@/components/page-transition'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { cn } from '@/lib/utils'

export type StatePanelTone = 'neutral' | 'danger' | 'warning'

const TONE_CLASS: Record<StatePanelTone, string> = {
  neutral: 'text-muted-foreground',
  danger: 'text-destructive',
  warning: 'text-warning',
}

/**
 * Shared frame for the three console state surfaces (ui-spec §11):
 * empty, error and loading all use the same minimum height, icon slot,
 * title/description rhythm and action area. Keeping the frame here is what
 * makes "no data", "failed" and "loading" read as one system instead of
 * three independently styled widgets.
 */
export function StatePanel(props: {
  icon: LucideIcon
  title: ReactNode
  description?: ReactNode
  action?: ReactNode
  tone?: StatePanelTone
  bordered?: boolean
  className?: string
  /** Stable test/telemetry hook for the state kind. */
  state: 'empty' | 'error' | 'loading'
}) {
  const Icon = props.icon
  return (
    <FadeIn>
      <Empty
        data-state-panel={props.state}
        className={cn(
          'min-h-[300px]',
          props.bordered && 'border',
          props.className
        )}
      >
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <Icon
              className={cn('size-6', TONE_CLASS[props.tone ?? 'neutral'])}
            />
          </EmptyMedia>
          <EmptyTitle>{props.title}</EmptyTitle>
          {props.description != null && (
            <EmptyDescription>{props.description}</EmptyDescription>
          )}
        </EmptyHeader>
        {props.action != null && <EmptyContent>{props.action}</EmptyContent>}
      </Empty>
    </FadeIn>
  )
}
