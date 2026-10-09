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
import { describe, expect, it } from 'vitest'

import {
  buildBucketAxis,
  defaultCustomRange,
  resolveTimeRange,
  startOfLocalDaySeconds,
} from '../time-range'

const DAY = 24 * 3600

/** 本地时间构造（避免测试机时区影响断言）。 */
const NOW = new Date(2026, 9, 10, 15, 30, 45)
const NOW_SECONDS = Math.floor(NOW.getTime() / 1000)
const TODAY_START = startOfLocalDaySeconds(NOW_SECONDS)

describe('resolveTimeRange presets', () => {
  it('24h is a rolling window ending now with hour granularity', () => {
    const range = resolveTimeRange({ preset: '24h' }, NOW)

    expect(range.from).toBe(NOW_SECONDS - DAY)
    expect(range.to).toBe(NOW_SECONDS)
    expect(range.granularity).toBe('hour')
  })

  it('today starts at the local midnight and ends now', () => {
    const range = resolveTimeRange({ preset: 'today' }, NOW)

    expect(range.from).toBe(TODAY_START)
    expect(range.to).toBe(NOW_SECONDS)
    expect(range.granularity).toBe('hour')
    // 今日与 24 小时口径不同：本地日界不是滚动 24h。
    expect(range.from).not.toBe(NOW_SECONDS - DAY)
  })

  it('last N days covers N calendar days including today with day granularity', () => {
    const three = resolveTimeRange({ preset: '3d' }, NOW)
    expect(three.from).toBe(TODAY_START - 2 * DAY)
    expect(three.to).toBe(NOW_SECONDS)
    expect(three.granularity).toBe('day')

    const seven = resolveTimeRange({ preset: '7d' }, NOW)
    expect(seven.from).toBe(TODAY_START - 6 * DAY)
    expect(seven.granularity).toBe('day')

    const thirty = resolveTimeRange({ preset: '30d' }, NOW)
    expect(thirty.from).toBe(TODAY_START - 29 * DAY)
    expect(thirty.granularity).toBe('day')
  })

  it('custom range includes both end days', () => {
    const start = new Date(2026, 9, 1)
    const end = new Date(2026, 9, 3)
    const range = resolveTimeRange({ preset: 'custom', start, end }, NOW)

    expect(range.from).toBe(
      startOfLocalDaySeconds(Math.floor(start.getTime() / 1000))
    )
    // 结束日含端：最后一秒（次日 00:00 之前）。
    expect(range.to).toBe(
      startOfLocalDaySeconds(Math.floor(end.getTime() / 1000)) + DAY - 1
    )
    // 3 个自然日跨度 > 2 天 → 日粒度。
    expect(range.granularity).toBe('day')
  })

  it('a single custom day stays on hour granularity', () => {
    const start = new Date(2026, 9, 5)
    const range = resolveTimeRange({ preset: 'custom', start, end: start }, NOW)

    expect(range.to - range.from).toBe(DAY - 1)
    expect(range.granularity).toBe('hour')
  })

  it('a custom range with inverted bounds collapses to the start day', () => {
    const start = new Date(2026, 9, 5)
    const end = new Date(2026, 9, 1)
    const range = resolveTimeRange({ preset: 'custom', start, end }, NOW)

    const startDay = startOfLocalDaySeconds(Math.floor(start.getTime() / 1000))
    expect(range.from).toBe(startDay)
    expect(range.to).toBe(startDay + DAY - 1)
  })

  it('exposes the previous window of equal length for the delta', () => {
    const range = resolveTimeRange({ preset: '7d' }, NOW)

    expect(range.previousTo).toBe(range.from)
    expect(range.previousTo - range.previousFrom).toBe(range.to - range.from)
  })

  it('defaultCustomRange spans seven calendar days including today', () => {
    const { start, end } = defaultCustomRange(NOW)

    expect(start.getHours()).toBe(0)
    expect(end.getHours()).toBe(0)
    expect(
      startOfLocalDaySeconds(Math.floor(end.getTime() / 1000)) -
        startOfLocalDaySeconds(Math.floor(start.getTime() / 1000))
    ).toBe(6 * DAY)
  })
})

describe('buildBucketAxis', () => {
  it('aligns hour buckets to the hour boundary', () => {
    const axis = buildBucketAxis(NOW_SECONDS - 3 * 3600, NOW_SECONDS, 'hour')

    expect(axis).toHaveLength(4)
    expect(axis[0] % 3600).toBe(0)
    expect(axis.at(-1)).toBe(Math.floor(NOW_SECONDS / 3600) * 3600)
  })

  it('aligns day buckets to the UTC day boundary used by the aggregate', () => {
    const from = TODAY_START - 2 * DAY
    const axis = buildBucketAxis(from, NOW_SECONDS, 'day')

    // 后端的日桶按 UTC 日界聚合，本地窗口的首尾可能落在半个 UTC 日里，
    // 因此桶数只保证覆盖窗口且逐个对齐、步长为一整天。
    expect(axis.length).toBeGreaterThanOrEqual(3)
    for (const ts of axis) expect(ts % DAY).toBe(0)
    expect(axis[0]).toBeLessThanOrEqual(from)
    expect(axis.at(-1)).toBeLessThanOrEqual(NOW_SECONDS)
    for (let index = 1; index < axis.length; index += 1) {
      expect(axis[index] - axis[index - 1]).toBe(DAY)
    }
  })
})
