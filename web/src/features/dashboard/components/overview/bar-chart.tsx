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
// 概览用迷你柱状图（ui-spec §6.2A 与 §11）。
//
// 实现已迁到共享的 `@/components/charts`（统一调色板、轴、网格与 tooltip），
// 这里只保留概览的调用约定（品牌绿柱、固定绘图高度），避免 dashboard 各卡片
// 各自拼一套图。新页面应直接使用共享组件。

import { BarChart, type BarChartDatum } from '@/components/charts'

export type OverviewBarChartDatum = BarChartDatum

export function OverviewBarChart(props: {
  data: OverviewBarChartDatum[]
  valueFormatter: (value: number) => string
  ariaLabel: string
}) {
  return (
    <BarChart
      data={props.data}
      valueFormatter={props.valueFormatter}
      ariaLabel={props.ariaLabel}
      tone='success'
    />
  )
}
