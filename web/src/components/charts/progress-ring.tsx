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
/**
 * Shared progress ring (ui-spec §11).
 *
 * One SVG implementation replaces the per-card rings in the overview's
 * service status card and any future ratio indicator. The value is clamped
 * to 0..1; the colour is a theme token (or a caller-chosen semantic colour)
 * so light/dark stay on the same mapping.
 */
import { cn } from '@/lib/utils'

import { CHART_TONE_VAR, type ChartTone } from './palette'

export type ProgressRingProps = {
  /** Ratio 0..1. Values outside the range are clamped. */
  value: number
  /** Accessible description for the ring (already translated by the caller). */
  ariaLabel: string
  tone?: ChartTone
  /** Explicit token colour; wins over `tone`. */
  color?: string
  className?: string
}

const SIZE = 24
const RADIUS = 9
const CIRCUMFERENCE = 2 * Math.PI * RADIUS

export function ProgressRing(props: ProgressRingProps) {
  const value = Number.isFinite(props.value)
    ? Math.min(1, Math.max(0, props.value))
    : 0
  const color = props.color ?? CHART_TONE_VAR[props.tone ?? 'success']

  return (
    <span
      role='img'
      aria-label={props.ariaLabel}
      className={cn(
        'inline-flex size-6 shrink-0 items-center justify-center',
        props.className
      )}
    >
      <svg viewBox={`0 0 ${SIZE} ${SIZE}`} className='size-6 -rotate-90'>
        <circle
          cx={SIZE / 2}
          cy={SIZE / 2}
          r={RADIUS}
          fill='none'
          strokeWidth='3'
          className='stroke-muted-foreground/25'
        />
        <circle
          cx={SIZE / 2}
          cy={SIZE / 2}
          r={RADIUS}
          fill='none'
          strokeWidth='3'
          strokeLinecap='round'
          stroke={color}
          strokeDasharray={`${CIRCUMFERENCE * value} ${CIRCUMFERENCE}`}
        />
      </svg>
    </span>
  )
}
