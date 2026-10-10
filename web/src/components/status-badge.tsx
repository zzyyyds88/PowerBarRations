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
/* eslint-disable react-refresh/only-export-components */
import * as React from 'react'

import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { stringToColor } from '@/lib/colors'
import { cn } from '@/lib/utils'

/**
 * Solid dot per variant (kept for the `showDot` compatibility path and as the
 * canonical key set for {@link StatusVariant}). Each entry names the same hue
 * slot the pill uses, so a dot rendered next to a pill can never disagree.
 */
export const dotColorMap = {
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-destructive',
  info: 'bg-info',
  neutral: 'bg-neutral',
  purple: 'bg-chart-5',
  amber: 'bg-warning',
  blue: 'bg-chart-1',
  cyan: 'bg-chart-1',
  green: 'bg-success',
  grey: 'bg-neutral',
  indigo: 'bg-chart-1',
  'light-blue': 'bg-info',
  'light-green': 'bg-success',
  lime: 'bg-chart-2',
  orange: 'bg-warning',
  pink: 'bg-chart-5',
  red: 'bg-destructive',
  teal: 'bg-chart-2',
  violet: 'bg-chart-5',
  yellow: 'bg-warning',
} as const

/**
 * Hue slot per variant, used as the tonal wash for the pill. The 21 variants
 * keep their original semantics (and the original collisions: blue/indigo
 * share `chart-1`, cyan/teal `chart-2`, purple/violet `chart-4`, amber/orange/
 * yellow `warning`, green `success`, red/danger `destructive`, neutral/grey
 * `neutral`); `light-blue` mirrors `info` and `light-green` mirrors `success`,
 * matching how the old text map already collapsed them.
 */
// variant → 色相槽。**名字必须与色相一致**（2026-10-10 订正）：此前 purple/violet
// 落在 chart-4（本调色板里是琥珀）、lime 落在 chart-3（红）、cyan 与 green 同槽，
// 导致"叫紫的显示成黄"。5 个数据色槽位固定为 蓝/绿/红/琥珀/紫，故：
// blue/indigo/cyan → 蓝；green/light-green/lime/teal → 绿；danger/red → 红；
// amber/orange/yellow/warning → 琥珀；purple/violet/pink → 紫。
const variantToneMap: Record<StatusVariant, string> = {
  success: 'pbr-tone-success',
  warning: 'pbr-tone-warning',
  danger: 'pbr-tone-destructive',
  info: 'pbr-tone-info',
  neutral: 'pbr-tone-neutral',
  purple: 'pbr-tone-chart-5',
  amber: 'pbr-tone-warning',
  blue: 'pbr-tone-chart-1',
  cyan: 'pbr-tone-chart-1',
  green: 'pbr-tone-success',
  grey: 'pbr-tone-neutral',
  indigo: 'pbr-tone-chart-1',
  'light-blue': 'pbr-tone-info',
  'light-green': 'pbr-tone-success',
  lime: 'pbr-tone-chart-2',
  orange: 'pbr-tone-warning',
  pink: 'pbr-tone-chart-5',
  red: 'pbr-tone-destructive',
  teal: 'pbr-tone-chart-2',
  violet: 'pbr-tone-chart-5',
  yellow: 'pbr-tone-warning',
}

/**
 * Label color per variant. These are the derived `--tone-*-text` tokens, so
 * the label follows the hue (presets included) and stays readable on the
 * tonal wash in both themes — the tokens are computed as a hue/ink mix whose
 * share is capped per theme for ≥4.5:1.
 */
export const textColorMap = {
  success: 'text-[var(--tone-success-text)]',
  warning: 'text-[var(--tone-warning-text)]',
  danger: 'text-[var(--tone-destructive-text)]',
  info: 'text-[var(--tone-info-text)]',
  neutral: 'text-[var(--tone-neutral-text)]',
  purple: 'text-[var(--tone-chart-4-text)]',
  amber: 'text-[var(--tone-warning-text)]',
  blue: 'text-[var(--tone-chart-1-text)]',
  cyan: 'text-[var(--tone-chart-2-text)]',
  green: 'text-[var(--tone-success-text)]',
  grey: 'text-[var(--tone-neutral-text)]',
  indigo: 'text-[var(--tone-chart-1-text)]',
  'light-blue': 'text-[var(--tone-info-text)]',
  'light-green': 'text-[var(--tone-success-text)]',
  lime: 'text-[var(--tone-chart-3-text)]',
  orange: 'text-[var(--tone-warning-text)]',
  pink: 'text-[var(--tone-chart-5-text)]',
  red: 'text-[var(--tone-destructive-text)]',
  teal: 'text-[var(--tone-chart-2-text)]',
  violet: 'text-[var(--tone-chart-4-text)]',
  yellow: 'text-[var(--tone-warning-text)]',
} as const

export type StatusVariant = keyof typeof dotColorMap

/** Controls the visual style of the badge.
 * - `badge`    — default pill with background and padding (default)
 * - `text`     — plain text, no background or padding, only color
 * - `underline`— plain text with a bottom border underline
 */
export type StatusBadgeType = 'badge' | 'text' | 'underline'

/** Context that lets ancestor components (e.g. MobileCardList field area)
 *  override the badge type without modifying every call site. */
export const StatusBadgeTypeContext =
  React.createContext<StatusBadgeType>('badge')

const sizeMap = {
  sm: 'h-5 gap-1 px-1.5 text-sm leading-none',
  md: 'h-5 gap-1 px-1.5 text-sm leading-none',
  lg: 'h-6 gap-1.5 px-2 text-sm leading-none',
} as const

const textSizeMap = {
  sm: 'gap-1 text-sm leading-none',
  md: 'gap-1 text-sm leading-none',
  lg: 'gap-1.5 text-sm leading-none',
} as const

export interface StatusBadgeProps extends Omit<
  React.HTMLAttributes<HTMLSpanElement>,
  'children'
> {
  label?: string
  children?: React.ReactNode
  icon?: LucideIcon
  pulse?: boolean
  /** Kept for compatibility. Badges no longer render leading dots. */
  showDot?: boolean
  variant?: StatusVariant | null
  size?: 'sm' | 'md' | 'lg' | null
  copyable?: boolean
  copyText?: string
  autoColor?: string
  /** Visual style. Defaults to 'badge'. Can be overridden via StatusBadgeTypeContext. */
  type?: StatusBadgeType
}

export function StatusBadge({
  label,
  children,
  icon: Icon,
  variant,
  size = 'sm',
  pulse = false,
  showDot = false,
  copyable = true,
  copyText,
  autoColor,
  type: typeProp,
  className,
  onClick,
  ...props
}: StatusBadgeProps) {
  const { copyToClipboard } = useCopyToClipboard()
  const contextType = React.useContext(StatusBadgeTypeContext)
  const type = typeProp ?? contextType

  const computedVariant: StatusVariant = autoColor
    ? (stringToColor(autoColor) as StatusVariant)
    : (variant ?? 'neutral')

  const handleClick = (e: React.MouseEvent<HTMLSpanElement>) => {
    if (copyable) {
      e.stopPropagation()
      copyToClipboard(copyText || label || '')
    }
    onClick?.(e)
  }

  const content =
    children ??
    (label ? (
      <span className='min-w-0 truncate leading-normal'>{label}</span>
    ) : null)

  const isBadge = type === 'badge'
  const title = copyable
    ? `Click to copy: ${copyText || label || ''}`
    : label || undefined

  return (
    <span
      data-slot='status-badge'
      className={cn(
        'inline-flex w-fit max-w-full min-w-0 shrink items-center font-medium tracking-normal whitespace-nowrap transition-colors',
        /* Pill = the shared tonal tile recipe (same-hue wash, same-hue
         * hairline, inner 1px highlight) over the variant's hue slot. */
        isBadge
          ? cn('pbr-tile border rounded-4xl', sizeMap[size ?? 'sm'])
          : cn(
              textSizeMap[size ?? 'sm'],
              type === 'underline' && 'border-b border-current pb-px'
            ),
        variantToneMap[computedVariant],
        textColorMap[computedVariant],
        pulse && 'animate-pulse',
        copyable &&
          'cursor-copy hover:brightness-95 active:scale-95 dark:hover:brightness-110',
        className
      )}
      onClick={handleClick}
      title={title}
      {...props}
    >
      {showDot && (
        <span
          className={cn(
            'inline-block size-1.5 shrink-0 rounded-full',
            dotColorMap[computedVariant]
          )}
          aria-hidden='true'
        />
      )}
      {Icon && <Icon className='size-3.5 shrink-0' />}
      {content}
    </span>
  )
}

export interface StatusBadgeListProps<T> extends Omit<
  React.HTMLAttributes<HTMLDivElement>,
  'children'
> {
  empty?: React.ReactNode
  getKey?: (item: T, index: number) => React.Key
  items: T[]
  max?: number
  moreLabel?: (remaining: number) => string
  renderItem: (item: T, index: number) => React.ReactNode
}

export function StatusBadgeList<T>(props: StatusBadgeListProps<T>) {
  const {
    className,
    empty = <span className='text-muted-foreground text-xs'>-</span>,
    getKey,
    items,
    max = 2,
    moreLabel,
    renderItem,
    ...domProps
  } = props

  if (items.length === 0) {
    return empty
  }

  const displayed = items.slice(0, max)
  const remaining = items.length - max

  return (
    <div
      className={cn(
        'flex max-w-full min-w-0 items-center gap-1 overflow-hidden',
        className
      )}
      {...domProps}
    >
      {displayed.map((item, index) => (
        <React.Fragment key={getKey?.(item, index) ?? index}>
          {renderItem(item, index)}
        </React.Fragment>
      ))}
      {remaining > 0 && (
        <StatusBadge
          label={moreLabel?.(remaining) ?? `+${remaining}`}
          variant='neutral'
          size='sm'
          copyable={false}
          className='shrink-0'
        />
      )}
    </div>
  )
}

export const statusPresets = {
  active: {
    variant: 'success' as const,
    label: 'Active',
  },
  inactive: {
    variant: 'neutral' as const,
    label: 'Inactive',
  },
  invited: {
    variant: 'info' as const,
    label: 'Invited',
  },
  suspended: {
    variant: 'danger' as const,
    label: 'Suspended',
  },
  pending: {
    variant: 'warning' as const,
    label: 'Pending',
    pulse: true,
  },
} as const

export type StatusPreset = keyof typeof statusPresets
