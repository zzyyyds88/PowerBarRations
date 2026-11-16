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
import type { ChannelModelPrice } from '../types'

// 渠道级上游单价的峰谷（可选时段）算术，与后端 `relaykit/dto.PriceBand`、
// `setting/pricing_setting.BandAt` 逐条对齐（design-v1 §16.9#7、api-spec §4.1、
// ui-spec §9）：
//
// - `days` 为 1–7（周一 = 1），表示时段的**起始日**；
// - `start`/`end` 为 HH:MM，左闭右开；`start > end` 表示跨午夜（从起始日
//   start 延续到次日 end，因此次日凌晨的命中由"前一天在 days 里"决定）；
// - `start == end` 是空区间，写路径拒绝；
// - 四个价格字段留空 = 继承行级兜底价，显式 0 = 该时段价格 0；
// - 按请求开始时间在 `timezone`（默认 Asia/Shanghai）下匹配**第一条**命中；
//   未命中用行级价兜底，日志 `price_band = -1`。

export const PRICE_FIELDS = [
  'input',
  'output',
  'cache_read',
  'cache_write',
] as const

export type PriceField = (typeof PRICE_FIELDS)[number]

/** 四个价格维度的展示文案键（i18n 键即英文文案，7 locale 均已提供）。 */
export const PRICE_FIELD_LABEL_KEYS: Record<PriceField, string> = {
  input: 'Input',
  output: 'Output',
  cache_read: 'Cache read',
  cache_write: 'Cache write',
}

/** 价格行未填 timezone 时使用的时区（与后端 `DefaultPriceTimezone` 一致）。 */
export const DEFAULT_PRICE_TIMEZONE = 'Asia/Shanghai'

/** 单个价格行允许的峰谷时段上限（与后端 `MaxPriceBandsPerModel` 一致）。 */
export const MAX_PRICE_BANDS = 64

/** 周一到周日（价格契约 Monday = 1 … Sunday = 7）。 */
export const WEEK_DAYS = [1, 2, 3, 4, 5, 6, 7] as const

export interface PriceBand {
  days: number[]
  start: string
  end: string
  input?: number
  output?: number
  cache_read?: number
  cache_write?: number
}

/**
 * 带峰谷的价格行：`ChannelModelPrice` 的可选扩展。
 *
 * `types.ts` 的 `ChannelModelPrice` 仍描述旧单层价（后端为同一 JSON 结构加了
 * 可选 `timezone`/`bands`）；控制台在价格编辑器内部按本类型读写，序列化时由
 * `normalizePricesForSetting` 保证旧行原样落库。
 */
export type PricedChannelModel = ChannelModelPrice & {
  timezone?: string
  bands?: PriceBand[]
}

export type BandIssue =
  | 'days_empty'
  | 'day_out_of_range'
  | 'start_invalid'
  | 'end_invalid'
  | 'start_equals_end'
  | 'negative_price'

export type RowIssue = 'timezone_invalid' | 'too_many_bands'

export interface PriceRowIssues {
  row: RowIssue[]
  bands: BandIssue[][]
}

/**
 * 严格解析 HH:MM（与后端 `ParseClockHHMM` 同口径）：只接受两位小时、两位
 * 分钟与半角冒号，范围 00:00–23:59。解析失败返回 null。
 */
export function parseClockHHMM(value: string): number | null {
  const trimmed = value.trim()
  if (trimmed.length !== 5 || trimmed[2] !== ':') return null
  for (const index of [0, 1, 3, 4]) {
    const char = trimmed[index]
    if (char < '0' || char > '9') return null
  }
  const hour = Number(trimmed.slice(0, 2))
  const minute = Number(trimmed.slice(3))
  if (hour > 23 || minute > 59) return null
  return hour * 60 + minute
}

/** 把一天内的分钟数格式化为 HH:MM（超出 0–1439 时按 24h 取模）。 */
export function formatClockHHMM(minutes: number): string {
  const normalized = ((minutes % 1440) + 1440) % 1440
  const hour = Math.floor(normalized / 60)
  const minute = normalized % 60
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
}

const timezoneFormatters = new Map<string, Intl.DateTimeFormat | null>()

function getTimezoneFormatter(timezone?: string): Intl.DateTimeFormat | null {
  const key = timezone?.trim() || DEFAULT_PRICE_TIMEZONE
  const cached = timezoneFormatters.get(key)
  if (cached !== undefined) return cached
  try {
    const formatter = new Intl.DateTimeFormat('en-US', {
      timeZone: key,
      weekday: 'short',
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
    })
    timezoneFormatters.set(key, formatter)
    return formatter
  } catch {
    // 容器镜像可能裁剪 tzdata；无效时区在 UI 里就地提示，匹配按兜底处理。
    timezoneFormatters.set(key, null)
    return null
  }
}

/** 空值视为合法（回落默认时区）；否则必须能被 Intl 解析。 */
export function isValidPriceTimezone(timezone?: string): boolean {
  const trimmed = timezone?.trim() ?? ''
  if (!trimmed) return true
  return getTimezoneFormatter(trimmed) !== null
}

const WEEKDAY_INDEX: Record<string, number> = {
  Mon: 1,
  Tue: 2,
  Wed: 3,
  Thu: 4,
  Fri: 5,
  Sat: 6,
  Sun: 7,
}

export interface LocalClock {
  day: number
  minutes: number
}

/** 把绝对时刻换算成价格行时区里的"星期几 + 一天内分钟数"。 */
export function localClock(at: Date, timezone?: string): LocalClock | null {
  const formatter = getTimezoneFormatter(timezone)
  if (!formatter) return null
  const parts = formatter.formatToParts(at)
  let day = 0
  let hour = Number.NaN
  let minute = Number.NaN
  for (const part of parts) {
    if (part.type === 'weekday') day = WEEKDAY_INDEX[part.value] ?? 0
    if (part.type === 'hour') hour = Number(part.value)
    if (part.type === 'minute') minute = Number(part.value)
  }
  if (!day || Number.isNaN(hour) || Number.isNaN(minute)) return null
  return { day, minutes: hour * 60 + minute }
}

/**
 * 判断 band 是否覆盖 `day` 的 `minutes`（与后端 `PriceBand.matches` 同口径）。
 * 跨午夜时，次日凌晨的命中由"前一天在 days 里"决定。
 */
export function bandCovers(
  band: PriceBand,
  day: number,
  minutes: number
): boolean {
  const start = parseClockHHMM(band.start)
  const end = parseClockHHMM(band.end)
  if (start === null || end === null) return false
  if (start < end) {
    return band.days.includes(day) && minutes >= start && minutes < end
  }
  if (start > end) {
    if (band.days.includes(day) && minutes >= start) return true
    const previousDay = day === 1 ? 7 : day - 1
    return band.days.includes(previousDay) && minutes < end
  }
  // start == end 是空区间（写路径已拒绝）；脏数据按不命中处理。
  return false
}

/** 返回命中的第一条 band 下标；未命中返回 -1（= 行级兜底价）。 */
export function matchBandIndex(
  bands: PriceBand[] | undefined,
  day: number,
  minutes: number
): number {
  if (!bands || bands.length === 0) return -1
  for (let index = 0; index < bands.length; index += 1) {
    if (bandCovers(bands[index], day, minutes)) return index
  }
  return -1
}

/**
 * 按 `at` 与价格行时区匹配第一条命中的 band；未命中、无 band 或时区不可解析
 * 一律返回 -1。
 */
export function matchBandAt(
  bands: PriceBand[] | undefined,
  at: Date,
  timezone?: string
): number {
  if (!bands || bands.length === 0) return -1
  const clock = localClock(at, timezone)
  if (!clock) return -1
  return matchBandIndex(bands, clock.day, clock.minutes)
}

export interface EffectivePrice {
  bandIndex: number
  clock: LocalClock | null
  input: number
  output: number
  cache_read: number
  cache_write: number
}

/**
 * 当前生效价：命中 band 时用 band 里显式填写的字段覆盖行级价，其余继承行级
 * 兜底价（与后端 `ModelPrice.PriceAt` 一致）；未命中返回行级价与 -1。
 */
export function effectivePriceAt(
  row: PricedChannelModel,
  at: Date
): EffectivePrice {
  const clock = localClock(at, row.timezone)
  const bandIndex = clock
    ? matchBandIndex(row.bands, clock.day, clock.minutes)
    : -1
  const band = bandIndex >= 0 ? row.bands?.[bandIndex] : undefined
  const resolve = (field: PriceField): number =>
    band?.[field] ?? row[field] ?? 0
  return {
    bandIndex,
    clock,
    input: resolve('input'),
    output: resolve('output'),
    cache_read: resolve('cache_read'),
    cache_write: resolve('cache_write'),
  }
}

export interface WeekCoverageCell {
  day: number
  hour: number
  bandIndex: number
}

function bandCoversHour(band: PriceBand, day: number, hour: number): boolean {
  const start = parseClockHHMM(band.start)
  const end = parseClockHHMM(band.end)
  if (start === null || end === null || start === end) return false
  const hourStart = hour * 60
  const hourEnd = hourStart + 60
  if (start < end) {
    return band.days.includes(day) && hourStart < end && hourEnd > start
  }
  const previousDay = day === 1 ? 7 : day - 1
  return (
    (band.days.includes(day) && hourEnd > start) ||
    (band.days.includes(previousDay) && hourStart < end)
  )
}

/**
 * 周视图网格数据：7 天 × 24 小时，每格记录覆盖该小时的第一条 band 下标
 * （-1 = 无 band 覆盖，该小时用行级兜底价）。跨午夜 band 会覆盖到次日凌晨
 * 的格子，但归属仍是起始日（与匹配口径一致）。
 */
export function buildWeekCoverage(
  bands: PriceBand[] | undefined
): WeekCoverageCell[] {
  const cells: WeekCoverageCell[] = []
  for (const day of WEEK_DAYS) {
    for (let hour = 0; hour < 24; hour += 1) {
      let bandIndex = -1
      if (bands) {
        for (let index = 0; index < bands.length; index += 1) {
          if (bandCoversHour(bands[index], day, hour)) {
            bandIndex = index
            break
          }
        }
      }
      cells.push({ day, hour, bandIndex })
    }
  }
  return cells
}

/** 单个 band 的结构问题（与后端 `PriceBand.Validate` 同口径）。 */
export function bandIssues(band: PriceBand): BandIssue[] {
  const issues: BandIssue[] = []
  if (band.days.length === 0) {
    issues.push('days_empty')
  } else if (
    band.days.some((day) => !Number.isInteger(day) || day < 1 || day > 7)
  ) {
    issues.push('day_out_of_range')
  }
  const start = parseClockHHMM(band.start)
  const end = parseClockHHMM(band.end)
  if (start === null) issues.push('start_invalid')
  if (end === null) issues.push('end_invalid')
  if (start !== null && end !== null && start === end) {
    issues.push('start_equals_end')
  }
  if (
    PRICE_FIELDS.some((field) => {
      const value = band[field]
      return value !== undefined && value < 0
    })
  ) {
    issues.push('negative_price')
  }
  return issues
}

/** 价格行的结构问题（timezone、band 数量与逐 band 问题）。 */
export function priceRowIssues(row: PricedChannelModel): PriceRowIssues {
  const issues: RowIssue[] = []
  if (!isValidPriceTimezone(row.timezone)) issues.push('timezone_invalid')
  if ((row.bands?.length ?? 0) > MAX_PRICE_BANDS) {
    issues.push('too_many_bands')
  }
  return {
    row: issues,
    bands: (row.bands ?? []).map(bandIssues),
  }
}

/**
 * DeepSeek 官方峰谷预设：**只填结构**（两个每天重复的时段 + 七天全选），
 * 不写死任何价格数值——金额由运维按实际采购价填写（ui-spec §9）。
 */
export function buildDeepSeekPeakPreset(): PriceBand[] {
  return [
    { days: [...WEEK_DAYS], start: '00:30', end: '08:30' },
    { days: [...WEEK_DAYS], start: '08:30', end: '00:30' },
  ]
}

/**
 * 把价格行写成 `setting.pbr_prices` 的落库形状。
 *
 * - 旧单层价（无 bands）**原样**输出 model + 四个数字，不强行生成 band；
 * - 只有 band、行级价全空的行也保留（band 价由用户填写，行级价按 0 兜底）；
 * - `timezone` 仅在非空时输出；band 价格仅在用户显式填写时输出（留空 =
 *   继承行级价，不能用 0 冒充）；
 * - band 的 `days` 归一为去重升序，时间只做首尾空白清理。结构错误不在前端
 *   静默修复——后端仍会以 `400 invalid_prices` 给出权威判定。
 */
export function normalizePricesForSetting(
  prices: PricedChannelModel[]
): Record<string, unknown>[] {
  return prices
    .map((price) => ({ ...price, model: price.model.trim() }))
    .filter((price) => price.model !== '')
    .filter(
      (price) =>
        [price.input, price.output, price.cache_read, price.cache_write].some(
          (value) => value !== undefined
        ) || (price.bands?.length ?? 0) > 0
    )
    .map((price) => {
      const setting: Record<string, unknown> = {
        model: price.model,
        input: price.input ?? 0,
        output: price.output ?? 0,
        cache_read: price.cache_read ?? 0,
        cache_write: price.cache_write ?? 0,
      }
      const timezone = price.timezone?.trim()
      if (timezone) setting.timezone = timezone
      const bands = price.bands ?? []
      if (bands.length > 0) {
        setting.bands = bands.map((band) => {
          const normalized: Record<string, unknown> = {
            days: [...new Set(band.days)].sort((a, b) => a - b),
            start: band.start.trim(),
            end: band.end.trim(),
          }
          for (const field of PRICE_FIELDS) {
            const value = band[field]
            if (value !== undefined) normalized[field] = value
          }
          return normalized
        })
      }
      return setting
    })
}
