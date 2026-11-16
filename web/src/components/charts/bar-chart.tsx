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
 * Shared div-based bar chart (ui-spec §11).
 *
 * The console does not ship a charting runtime for single-series bars: a
 * flex row of token-coloured blocks is lighter and renders identically on
 * both themes. All bars now come through this component so the axis, grid,
 * hover tooltip and palette are defined once. `@visactor`/`recharts` remain
 * available for future multi-series surfaces; they must use `palette.ts`.
 *
 * Every bar carries its value in `aria-label` on the plot's `role='img'`
 * wrapper; hover/focus shows the same text through one tooltip surface.
 */
import { cn } from '@/lib/utils'

import {
  CHART_SERIES_BAR_CLASS,
  CHART_TONE_BAR_CLASS,
  type ChartSeriesIndex,
  type ChartTone,
} from './palette'

export interface BarChartDatum {
  key: string
  /** X-axis label (date/hour or group name). */
  label: string
  value: number
  /** Tooltip text; falls back to `label · formatted value`. */
  title?: string
}

/** X-axis labels beyond this count are thinned to avoid collisions. */
const X_LABEL_LIMIT = 6
/** Bar width ceiling: one or two groups must not become a full-width block. */
const BAR_MAX_WIDTH_CLASS = 'max-w-12'
/** Visible labels are at least 1/6 of the plot apart, so they can be wide. */
const X_LABEL_MAX_WIDTH_CLASS = 'max-w-32'
/** Past this bar count the last label hugs the right edge instead of centring. */
const DENSE_AXIS_BAR_COUNT = 12
const NICE_STEPS = [1, 2, 2.5, 5, 10]

/** Raise 0..max to a "nice" axis top with four integer-ish divisions. */
function niceAxisMax(max: number): number {
  if (!Number.isFinite(max) || max <= 0) return 1
  const raw = max / 4
  const exponent = Math.floor(Math.log10(raw))
  const base = 10 ** exponent
  const normalized = raw / base
  const step = NICE_STEPS.find((candidate) => normalized <= candidate) ?? 10
  return step * base * 4
}

/** Label only the last N bars so narrow cards never overlap text. */
function visibleLabelIndexes(count: number): Set<number> {
  const step = Math.max(1, Math.ceil(count / X_LABEL_LIMIT))
  const indexes = new Set<number>()
  for (let index = count - 1; index >= 0; index -= step) {
    indexes.add(index)
  }
  return indexes
}

export type BarChartProps = {
  data: BarChartDatum[]
  valueFormatter: (value: number) => string
  ariaLabel: string
  /** Semantic tone for status-ish bars. Ignored when `series` is set. */
  tone?: ChartTone
  /** Palette slot 1..5; wins over `tone`. */
  series?: ChartSeriesIndex
  className?: string
}

export function BarChart(props: BarChartProps) {
  let max = 0
  for (const datum of props.data) {
    if (Number.isFinite(datum.value)) max = Math.max(max, datum.value)
  }
  const axisMax = niceAxisMax(max)
  const ticks = [1, 0.75, 0.5, 0.25, 0].map((ratio) => ({
    ratio,
    value: axisMax * ratio,
  }))
  const labelIndexes = visibleLabelIndexes(props.data.length)
  const barClass =
    props.series !== undefined
      ? CHART_SERIES_BAR_CLASS[props.series]
      : CHART_TONE_BAR_CLASS[props.tone ?? 'success']

  return (
    <div className={cn('flex min-w-0 gap-2', props.className)}>
      <div
        aria-hidden='true'
        className='text-muted-foreground text-caption flex h-(--chart-plot-height) shrink-0 flex-col justify-between text-right leading-none tabular-nums'
      >
        {ticks.map((tick) => (
          <span key={tick.ratio}>{props.valueFormatter(tick.value)}</span>
        ))}
      </div>
      <div className='min-w-0 flex-1'>
        <div className='relative h-(--chart-plot-height)'>
          {ticks.map((tick) => (
            <div
              key={tick.ratio}
              aria-hidden='true'
              className='border-border/60 absolute inset-x-0 border-t'
              style={{ top: `${(1 - tick.ratio) * 100}%` }}
            />
          ))}
          <div
            role='img'
            aria-label={props.ariaLabel}
            className='absolute inset-0 flex gap-1'
          >
            {props.data.map((datum) => {
              const ratio = axisMax > 0 ? datum.value / axisMax : 0
              const tooltip =
                datum.title ??
                `${datum.label} · ${props.valueFormatter(datum.value)}`
              return (
                // The outer slot fills the plot height and centres the bar, so
                // the bar keeps a width ceiling even with a single group.
                <div
                  key={datum.key}
                  className='group/bar relative flex min-w-0 flex-1 items-end justify-center'
                >
                  <span
                    role='tooltip'
                    className='bg-popover text-popover-foreground border-border text-caption rounded-control shadow-popover pointer-events-none absolute bottom-full z-10 mb-1 hidden max-w-48 truncate border px-2 py-1 whitespace-nowrap group-hover/bar:block'
                  >
                    {tooltip}
                  </span>
                  <div
                    aria-hidden='true'
                    className={cn(
                      'w-full rounded-t-sm transition-colors',
                      BAR_MAX_WIDTH_CLASS,
                      ratio <= 0 ? 'bg-muted' : barClass
                    )}
                    style={{
                      height:
                        ratio > 0 ? `${Math.max(2, ratio * 100)}%` : '2px',
                    }}
                  />
                </div>
              )
            })}
          </div>
        </div>
        {/* Labels are absolutely positioned at each bar's centre. The last
            label hugs the right edge on dense axes (centring would overflow). */}
        <div
          aria-hidden='true'
          className='text-muted-foreground text-caption relative mt-1 h-3.5 overflow-hidden leading-tight'
        >
          {props.data.map((datum, index) => {
            if (!labelIndexes.has(index)) return null
            const clampToRightEdge =
              index === props.data.length - 1 &&
              props.data.length > DENSE_AXIS_BAR_COUNT
            return (
              <span
                key={datum.key}
                title={datum.label}
                className={cn(
                  'absolute top-0 truncate',
                  X_LABEL_MAX_WIDTH_CLASS,
                  !clampToRightEdge && '-translate-x-1/2'
                )}
                style={
                  clampToRightEdge
                    ? { right: 0 }
                    : {
                        left: `${((index + 0.5) / props.data.length) * 100}%`,
                      }
                }
              >
                {datum.label}
              </span>
            )
          })}
        </div>
        {/* Same values as the hover tooltips, exposed to screen readers
            without adding one tab stop per bar. */}
        <ul className='sr-only'>
          {props.data.map((datum) => (
            <li key={datum.key}>
              {datum.title ??
                `${datum.label} · ${props.valueFormatter(datum.value)}`}
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}
