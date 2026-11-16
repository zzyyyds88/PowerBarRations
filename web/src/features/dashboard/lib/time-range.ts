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
// 首页概览的时间分档（ui-spec §6.2A）。
//
// 整页共用一个档位，换算成 Unix 秒后驱动所有卡片：
// - `24h`   = 滚动 24 小时（now-24h → now）；
// - `today` = 本地自然日 00:00 → now；
// - `3d` / `7d` / `30d` = 含今日的 N 个自然日 00:00 → now；
// - `custom` = 用户给的起止日期，**含端**（起始日 00:00 → 结束日 23:59:59）。
//
// 粒度：区间跨度 > 2 天用 `day`，否则 `hour`（api-spec §5.5 的 granularity）。
// 环比窗口 = 与当前窗口等长且紧邻的前一窗口（previousTo = from）。

export type PBRTimeRangePreset =
  | '24h'
  | 'today'
  | '3d'
  | '7d'
  | '30d'
  | 'custom'

export type PBRTimeRangeGranularity = 'hour' | 'day'

export interface PBRTimeRangeSelection {
  preset: PBRTimeRangePreset
  /** 仅 `custom` 使用：起始日（含端，本地日界）。 */
  start?: Date
  /** 仅 `custom` 使用：结束日（含端，本地日界）。 */
  end?: Date
}

export interface PBRResolvedTimeRange {
  preset: PBRTimeRangePreset
  from: number
  to: number
  granularity: PBRTimeRangeGranularity
  /** 上一等长窗口的起点（环比基准）。 */
  previousFrom: number
  /** 上一等长窗口的终点（= 当前窗口的 from）。 */
  previousTo: number
}

/** 档位取值域；标签由组件以字面量 t('...') 提供，便于 i18n 扫描。 */
export const PBR_TIME_RANGE_PRESETS: PBRTimeRangePreset[] = [
  '24h',
  'today',
  '3d',
  '7d',
  '30d',
  'custom',
]

const HOUR_SECONDS = 3600
const DAY_SECONDS = 24 * HOUR_SECONDS
const TWO_DAYS_SECONDS = 2 * DAY_SECONDS
/** 桶轴上界：防止超长自定义区间生成过大的数组。 */
const MAX_BUCKETS = 400

/** 某个时刻所在本地自然日的 00:00（Unix 秒）。 */
export function startOfLocalDaySeconds(tsSeconds: number): number {
  const day = new Date(tsSeconds * 1000)
  day.setHours(0, 0, 0, 0)
  return Math.floor(day.getTime() / 1000)
}

/** 以 `now` 为基准往前 `daysBack` 个自然日的 00:00（Unix 秒），按本地日界推进。 */
function localDayStartSeconds(now: Date, daysBack: number): number {
  const day = new Date(now.getTime())
  day.setHours(0, 0, 0, 0)
  day.setDate(day.getDate() - daysBack)
  return Math.floor(day.getTime() / 1000)
}

/** 自定义区间的默认取值：含今日的最近 7 个自然日。 */
export function defaultCustomRange(now: Date = new Date()): {
  start: Date
  end: Date
} {
  const start = new Date(now.getTime())
  start.setHours(0, 0, 0, 0)
  start.setDate(start.getDate() - 6)
  const end = new Date(now.getTime())
  end.setHours(0, 0, 0, 0)
  return { start, end }
}

/** 把档位选择换算成 from/to（Unix 秒）与粒度。 */
export function resolveTimeRange(
  selection: PBRTimeRangeSelection,
  now: Date = new Date()
): PBRResolvedTimeRange {
  const nowSeconds = Math.floor(now.getTime() / 1000)
  let from: number
  let to: number

  switch (selection.preset) {
    case '24h':
      to = nowSeconds
      from = to - DAY_SECONDS
      break
    case 'today':
      from = localDayStartSeconds(now, 0)
      to = nowSeconds
      break
    case '3d':
      from = localDayStartSeconds(now, 2)
      to = nowSeconds
      break
    case '7d':
      from = localDayStartSeconds(now, 6)
      to = nowSeconds
      break
    case '30d':
      from = localDayStartSeconds(now, 29)
      to = nowSeconds
      break
    case 'custom': {
      const start = selection.start ?? now
      const end = selection.end ?? start
      from = startOfLocalDaySeconds(Math.floor(start.getTime() / 1000))
      const endDayStart = startOfLocalDaySeconds(
        Math.floor(end.getTime() / 1000)
      )
      // 结束日含端：取该自然日的最后一秒；起止倒挂时收敛为「起始日一整天」。
      to =
        endDayStart >= from
          ? endDayStart + DAY_SECONDS - 1
          : from + DAY_SECONDS - 1
      break
    }
  }

  const granularity: PBRTimeRangeGranularity =
    to - from > TWO_DAYS_SECONDS ? 'day' : 'hour'
  const span = Math.max(1, to - from)

  return {
    preset: selection.preset,
    from,
    to,
    granularity,
    previousFrom: from - span,
    previousTo: from,
  }
}

/**
 * 生成时间轴的桶起点数组（与后端聚合桶对齐：小时桶按整点、日桶按 UTC 日界）。
 * 后端只返回非空桶，这里补齐空桶，趋势图才不会把缺口挤掉。
 */
export function buildBucketAxis(
  from: number,
  to: number,
  granularity: PBRTimeRangeGranularity
): number[] {
  const step = granularity === 'day' ? DAY_SECONDS : HOUR_SECONDS
  const first = Math.floor(from / step) * step
  const last = Math.floor(to / step) * step
  const axis: number[] = []
  for (let ts = first; ts <= last && axis.length < MAX_BUCKETS; ts += step) {
    axis.push(ts)
  }
  return axis
}
