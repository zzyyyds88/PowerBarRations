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
 * The console's single chart surface (ui-spec §11).
 * Import charts from this barrel so a palette or geometry change lands in
 * one place.
 */
export { BarChart, type BarChartDatum, type BarChartProps } from './bar-chart'
export { ProgressRing, type ProgressRingProps } from './progress-ring'
export {
  CHART_SERIES_BAR_CLASS,
  CHART_TONE_BAR_CLASS,
  CHART_TONE_VAR,
  type ChartSeriesIndex,
  type ChartTone,
} from './palette'
