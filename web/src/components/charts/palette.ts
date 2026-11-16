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
 * The single chart palette (ui-spec §11).
 *
 * Series colours are the `--chart-1..5` theme tokens (blue / green / red /
 * amber / violet, with a dark-theme lift defined in theme.css), so a theme
 * preset repaints charts without touching chart code. Semantic tones map to
 * the status tokens. Charts never spell a hex value or a raw Tailwind colour.
 */
export type ChartSeriesIndex = 1 | 2 | 3 | 4 | 5

/** Tailwind classes for the series palette, for CSS-drawn (div) charts. */
export const CHART_SERIES_BAR_CLASS: Record<ChartSeriesIndex, string> = {
  1: 'bg-chart-1/75 hover:bg-chart-1',
  2: 'bg-chart-2/75 hover:bg-chart-2',
  3: 'bg-chart-3/75 hover:bg-chart-3',
  4: 'bg-chart-4/75 hover:bg-chart-4',
  5: 'bg-chart-5/75 hover:bg-chart-5',
}

export type ChartTone = 'success' | 'warning' | 'danger' | 'neutral'

/** Semantic status colours, as CSS variables (SVG / inline styles). */
export const CHART_TONE_VAR: Record<ChartTone, string> = {
  success: 'var(--success)',
  warning: 'var(--warning)',
  danger: 'var(--destructive)',
  neutral: 'var(--muted-foreground)',
}

/** The same semantic tones, as Tailwind fill classes. */
export const CHART_TONE_BAR_CLASS: Record<ChartTone, string> = {
  success: 'bg-success/75 hover:bg-success',
  warning: 'bg-warning/75 hover:bg-warning',
  danger: 'bg-destructive/75 hover:bg-destructive',
  neutral: 'bg-muted-foreground/35 hover:bg-muted-foreground/55',
}
