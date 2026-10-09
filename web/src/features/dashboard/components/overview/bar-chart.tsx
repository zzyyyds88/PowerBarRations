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
// 概览用迷你柱状图（ui-spec §6.2A：品牌绿柱、X 轴日期/小时、Y 轴带刻度与浅网格线）。
//
// 沿用本项目既有的 div/色块小图风格（见 dashboard 的成本趋势图与 StatCard），
// 不引入图表库依赖：Y 轴刻度用「好看整数值」算轴顶，网格线用 absolute 定位的
// 上边框，柱子是 flex 等分色块。每个柱子带 title，鼠标悬停可读精确值。

import { cn } from '@/lib/utils'

export interface OverviewBarChartDatum {
  key: string
  /** X 轴文案（日期/小时或分组名）。 */
  label: string
  value: number
  /** 悬停提示（缺省用 label + 值）。 */
  title?: string
}

const PLOT_HEIGHT_CLASS = 'h-[200px]'
/** X 轴最多显示的标签数量（其余靠悬停 title 读取）。 */
const X_LABEL_LIMIT = 6
const NICE_STEPS = [1, 2, 2.5, 5, 10]

/** 把 0~max 抬到「好看」的轴顶（4 等分，刻度落在整数级步长上）。 */
function niceAxisMax(max: number): number {
  if (!Number.isFinite(max) || max <= 0) return 1
  const raw = max / 4
  const exponent = Math.floor(Math.log10(raw))
  const base = 10 ** exponent
  const normalized = raw / base
  const step = NICE_STEPS.find((candidate) => normalized <= candidate) ?? 10
  return step * base * 4
}

/** 只给最后若干根柱子加 X 轴标签，避免窄卡片里文字互相压叠。 */
function visibleLabelIndexes(count: number): Set<number> {
  const step = Math.max(1, Math.ceil(count / X_LABEL_LIMIT))
  const indexes = new Set<number>()
  for (let index = count - 1; index >= 0; index -= step) {
    indexes.add(index)
  }
  return indexes
}

export function OverviewBarChart(props: {
  data: OverviewBarChartDatum[]
  valueFormatter: (value: number) => string
  ariaLabel: string
}) {
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

  return (
    <div className='flex min-w-0 gap-2'>
      <div
        aria-hidden='true'
        className={cn(
          'text-muted-foreground flex shrink-0 flex-col justify-between text-right text-[10px] leading-none tabular-nums',
          PLOT_HEIGHT_CLASS
        )}
      >
        {ticks.map((tick) => (
          <span key={tick.ratio}>{props.valueFormatter(tick.value)}</span>
        ))}
      </div>
      <div className='min-w-0 flex-1'>
        <div className={cn('relative', PLOT_HEIGHT_CLASS)}>
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
            className='absolute inset-0 flex items-end gap-1'
          >
            {props.data.map((datum) => {
              const ratio = axisMax > 0 ? datum.value / axisMax : 0
              return (
                <div
                  key={datum.key}
                  title={
                    datum.title ??
                    `${datum.label} · ${props.valueFormatter(datum.value)}`
                  }
                  className={cn(
                    'bg-success/70 hover:bg-success min-w-[2px] flex-1 rounded-t-sm transition-colors',
                    ratio <= 0 && 'bg-muted'
                  )}
                  style={{
                    height: ratio > 0 ? `${Math.max(2, ratio * 100)}%` : '2px',
                  }}
                />
              )
            })}
          </div>
        </div>
        <div
          aria-hidden='true'
          className='text-muted-foreground mt-1 flex gap-1 text-[10px] leading-tight'
        >
          {props.data.map((datum, index) => (
            <span
              key={datum.key}
              className='min-w-0 flex-1 truncate text-center'
              title={datum.label}
            >
              {labelIndexes.has(index) ? datum.label : '\u00A0'}
            </span>
          ))}
        </div>
      </div>
    </div>
  )
}
