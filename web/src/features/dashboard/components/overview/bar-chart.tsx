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
/** 柱子最大宽度：1~2 个分组时柱子不会撑成一整块背景板（48px）。 */
const BAR_MAX_WIDTH_CLASS = 'max-w-12'
/** 标签最大宽度：可见标签间距至少是绘图区的 1/6，留 2rem 余量即不会互相压叠（128px）。 */
const X_LABEL_MAX_WIDTH_CLASS = 'max-w-32'
/** 超过这个柱数，末位柱离绘图区右沿已不足半个标签宽，末位标签要贴右沿不能居中。 */
const DENSE_AXIS_BAR_COUNT = 12
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
              className='absolute inset-x-0 border-t border-[color-mix(in_oklab,var(--border)_60%,transparent)]'
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
              return (
                // 外层是「分组槽」：撑满绘图区高度并居中柱子，因此柱子宽度可以有上限
                // （1 个分组时柱子仍是 48px 而不是 818px 宽的一整块）。
                <div
                  key={datum.key}
                  title={
                    datum.title ??
                    `${datum.label} · ${props.valueFormatter(datum.value)}`
                  }
                  className='flex min-w-0 flex-1 items-end justify-center'
                >
                  <div
                    className={cn(
                      'bg-success/70 hover:bg-success w-full rounded-t-sm transition-colors',
                      BAR_MAX_WIDTH_CLASS,
                      ratio <= 0 && 'bg-muted'
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
        {/* X 轴标签绝对定位到各自柱子的中心：可见标签之间至少隔 1/6 个绘图区宽度，
            所以标签可以有真实宽度。原来是每根柱子一个 flex-1 槽，25 根小时柱每槽只有
            30px，标签全被截成 "Oct ..."。末位标签在密集轴上贴右沿（居中会越出绘图区），
            稀疏轴上仍居中在柱子上；标签过宽时由 overflow-hidden 兜底。 */}
        <div
          aria-hidden='true'
          className='text-muted-foreground relative mt-1 h-3.5 overflow-hidden text-[10px] leading-tight'
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
      </div>
    </div>
  )
}
