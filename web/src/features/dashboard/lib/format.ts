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
// 首页概览的展示格式化（ui-spec §6.2A）。
//
// 空样本统一显示 `-`（不是 0），避免"没数据"被误读成"数据是 0"。

const EMPTY = '-'

/** Token 总数缩写：`15.1B` / `556.4M` / `12.3K`。 */
export function formatTokenAmount(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0'
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(1)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return String(Math.round(value))
}

/** 平均延迟：`840ms` / `1.20s`；无样本为 `-`。 */
export function formatLatencyMs(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return EMPTY
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}s`
  return `${Math.round(value)}ms`
}

/** 吞吐量（output_tps，后缀 tps）：`41.2 tps`；无样本为 `-`。 */
export function formatThroughputTps(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return EMPTY
  return `${value.toFixed(1)} tps`
}

/** 成功率（0~1 小数）：`98.33%`；无样本为 `-`。 */
export function formatSuccessRate(value: number): string {
  if (!Number.isFinite(value)) return EMPTY
  return `${(value * 100).toFixed(2)}%`
}

/** 调用次数等整数：千分位；非法值为 `-`。 */
export function formatCount(value: number): string {
  if (!Number.isFinite(value)) return EMPTY
  return new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 0,
  }).format(value)
}

/**
 * 环比（%）：`previous <= 0` 时没有可比基准，返回 null（展示为 `-`）。
 */
export function deltaPercent(current: number, previous: number): number | null {
  if (!Number.isFinite(current) || !Number.isFinite(previous)) return null
  if (previous <= 0) return null
  return ((current - previous) / previous) * 100
}

/** 环比文案：`+12.3%` / `-4.0%`；无基准为 `-`。 */
export function formatDeltaPercent(value: number | null): string {
  if (value == null || !Number.isFinite(value)) return EMPTY
  const sign = value > 0 ? '+' : ''
  return `${sign}${value.toFixed(1)}%`
}

/** 无真实 logo 时的首字母占位（取首个非空字符，大写）。 */
export function modelInitial(name: string): string {
  const trimmed = name.trim()
  if (trimmed === '') return '?'
  return trimmed.slice(0, 1).toUpperCase()
}
