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
import { describe, expect, test } from 'vitest'

import {
  DEFAULT_PRICE_TIMEZONE,
  MAX_PRICE_BANDS,
  bandCovers,
  bandIssues,
  buildDeepSeekPeakPreset,
  buildWeekCoverage,
  effectivePriceAt,
  formatClockHHMM,
  localClock,
  matchBandAt,
  matchBandIndex,
  normalizePricesForSetting,
  parseClockHHMM,
  priceRowIssues,
  type PriceBand,
} from '../channel-prices'

// 峰谷价格算术与后端 `setting/pricing_setting` 对齐（design-v1 §16.9#7、
// api-spec §4.1、ui-spec §9）：days 指时段起始日、跨午夜归起始日、
// start > end 表示跨午夜、未命中用行级兜底价且 price_band = -1。

function band(overrides: Partial<PriceBand>): PriceBand {
  return { days: [1], start: '00:00', end: '01:00', ...overrides }
}

describe('HH:MM 解析与格式化', () => {
  test('接受两位小时与分钟的 HH:MM', () => {
    expect(parseClockHHMM('00:00')).toBe(0)
    expect(parseClockHHMM('08:30')).toBe(510)
    expect(parseClockHHMM('23:59')).toBe(1439)
  })

  test('拒绝非严格写法与越界值', () => {
    for (const value of ['9:00', '24:00', '00:60', '0000', '', '09-00']) {
      expect(parseClockHHMM(value)).toBeNull()
    }
  })

  test('首尾空白按后端规则容忍', () => {
    expect(parseClockHHMM(' 09:00 ')).toBe(540)
  })

  test('格式化回 HH:MM', () => {
    expect(formatClockHHMM(0)).toBe('00:00')
    expect(formatClockHHMM(510)).toBe('08:30')
    expect(formatClockHHMM(1439)).toBe('23:59')
  })
})

describe('时段匹配', () => {
  test('同日时段左闭右开', () => {
    const entry = band({ days: [1], start: '09:00', end: '18:00' })
    expect(bandCovers(entry, 1, 9 * 60)).toBe(true)
    expect(bandCovers(entry, 1, 18 * 60 - 1)).toBe(true)
    expect(bandCovers(entry, 1, 18 * 60)).toBe(false)
    expect(bandCovers(entry, 1, 8 * 60 + 59)).toBe(false)
    expect(bandCovers(entry, 2, 10 * 60)).toBe(false)
  })

  test('跨午夜时段归属起始日：次日凌晨由前一天命中', () => {
    const entry = band({ days: [1], start: '22:00', end: '02:00' })
    expect(bandCovers(entry, 1, 22 * 60)).toBe(true)
    expect(bandCovers(entry, 1, 23 * 60 + 59)).toBe(true)
    expect(bandCovers(entry, 2, 0)).toBe(true)
    expect(bandCovers(entry, 2, 2 * 60 - 1)).toBe(true)
    expect(bandCovers(entry, 2, 2 * 60)).toBe(false)
    // 周二当天 22:00 不命中：band 只属于周一。
    expect(bandCovers(entry, 2, 22 * 60)).toBe(false)
  })

  test('周日跨到周一的边界', () => {
    const entry = band({ days: [7], start: '23:00', end: '01:00' })
    expect(bandCovers(entry, 1, 0)).toBe(true)
    expect(bandCovers(entry, 1, 30)).toBe(true)
    expect(bandCovers(entry, 1, 60)).toBe(false)
  })

  test('重叠时段先命中者生效', () => {
    const bands = [
      band({ days: [1], start: '00:00', end: '12:00' }),
      band({ days: [1], start: '06:00', end: '18:00' }),
    ]
    expect(matchBandIndex(bands, 1, 8 * 60)).toBe(0)
    expect(matchBandIndex(bands, 1, 13 * 60)).toBe(1)
    expect(matchBandIndex(bands, 1, 19 * 60)).toBe(-1)
  })

  test('空区间与非法时间不命中', () => {
    expect(bandCovers(band({ start: '09:00', end: '09:00' }), 1, 540)).toBe(
      false
    )
    expect(bandCovers(band({ start: '9:00', end: '18:00' }), 1, 540)).toBe(
      false
    )
  })
})

describe('时区换算', () => {
  test('按 IANA 时区得到本地星期与分钟', () => {
    // 2026-11-06T00:30Z = 上海 2026-11-06（周五）08:30。
    expect(
      localClock(new Date('2026-11-06T00:30:00Z'), 'Asia/Shanghai')
    ).toEqual({ day: 5, minutes: 8 * 60 + 30 })
    expect(localClock(new Date('2026-11-06T00:30:00Z'), 'UTC')).toEqual({
      day: 5,
      minutes: 30,
    })
  })

  test('空时区回落 Asia/Shanghai，非法时区返回 null', () => {
    expect(localClock(new Date('2026-11-06T00:30:00Z'), '')).toEqual({
      day: 5,
      minutes: 8 * 60 + 30,
    })
    expect(
      localClock(new Date('2026-11-06T00:30:00Z'), 'Mars/Olympus')
    ).toBeNull()
  })

  test('按请求开始时间匹配 band（时区参与）', () => {
    const bands = [band({ days: [5], start: '08:00', end: '09:00' })]
    expect(
      matchBandAt(bands, new Date('2026-11-06T00:30:00Z'), 'Asia/Shanghai')
    ).toBe(0)
    expect(matchBandAt(bands, new Date('2026-11-06T00:30:00Z'), 'UTC')).toBe(-1)
  })
})

describe('当前生效价', () => {
  const row = {
    model: 'gpt-4o',
    input: 10,
    output: 20,
    timezone: 'Asia/Shanghai',
    bands: [
      band({
        days: [5],
        start: '08:00',
        end: '09:00',
        input: 5,
        cache_read: 0,
      }),
    ],
  }

  test('命中 band 时显式字段覆盖行级价，其余继承', () => {
    const effective = effectivePriceAt(row, new Date('2026-11-06T00:30:00Z'))
    expect(effective.bandIndex).toBe(0)
    expect(effective.input).toBe(5)
    expect(effective.output).toBe(20)
    expect(effective.cache_read).toBe(0)
    expect(effective.cache_write).toBe(0)
    expect(effective.clock).toEqual({ day: 5, minutes: 510 })
  })

  test('未命中时返回行级价与 price_band -1', () => {
    const effective = effectivePriceAt(row, new Date('2026-11-06T05:00:00Z'))
    expect(effective.bandIndex).toBe(-1)
    expect(effective.input).toBe(10)
    expect(effective.output).toBe(20)
  })

  test('旧单层价（无 bands）恒为 -1', () => {
    const effective = effectivePriceAt(
      { model: 'gpt-4o', input: 1, output: 2 },
      new Date('2026-11-06T00:30:00Z')
    )
    expect(effective.bandIndex).toBe(-1)
    expect(effective.input).toBe(1)
    expect(effective.output).toBe(2)
  })
})

describe('周视图覆盖', () => {
  test('7 天 × 24 小时，跨午夜覆盖次日凌晨但归属起始日', () => {
    const cells = buildWeekCoverage([
      band({ days: [1], start: '22:00', end: '02:00' }),
    ])
    expect(cells).toHaveLength(7 * 24)
    const at = (day: number, hour: number) =>
      cells.find((cell) => cell.day === day && cell.hour === hour)?.bandIndex
    expect(at(1, 21)).toBe(-1)
    expect(at(1, 22)).toBe(0)
    expect(at(2, 1)).toBe(0)
    expect(at(2, 2)).toBe(-1)
    expect(at(2, 22)).toBe(-1)
  })

  test('重叠时段在每格显示第一条命中', () => {
    const cells = buildWeekCoverage([
      band({ days: [1], start: '00:00', end: '12:00' }),
      band({ days: [1], start: '06:00', end: '18:00' }),
    ])
    const at = (day: number, hour: number) =>
      cells.find((cell) => cell.day === day && cell.hour === hour)?.bandIndex
    expect(at(1, 8)).toBe(0)
    expect(at(1, 13)).toBe(1)
    expect(at(2, 8)).toBe(-1)
  })

  test('无 bands 时全部为兜底', () => {
    expect(buildWeekCoverage([]).every((cell) => cell.bandIndex === -1)).toBe(
      true
    )
  })
})

describe('结构校验', () => {
  test('band 问题与后端校验同口径', () => {
    expect(bandIssues(band({ days: [] }))).toEqual(['days_empty'])
    expect(bandIssues(band({ days: [0] }))).toEqual(['day_out_of_range'])
    expect(bandIssues(band({ start: '9:00' }))).toContain('start_invalid')
    expect(bandIssues(band({ start: '10:00', end: '10:00' }))).toEqual([
      'start_equals_end',
    ])
    expect(bandIssues(band({ input: -1 }))).toEqual(['negative_price'])
    expect(bandIssues(band({}))).toEqual([])
  })

  test('价格行问题：非法时区与超过 64 条', () => {
    expect(
      priceRowIssues({ model: 'm', timezone: 'Mars/Olympus' }).row
    ).toEqual(['timezone_invalid'])
    const tooMany = {
      model: 'm',
      bands: Array.from({ length: MAX_PRICE_BANDS + 1 }, () => band({})),
    }
    expect(priceRowIssues(tooMany).row).toEqual(['too_many_bands'])
    expect(priceRowIssues({ model: 'm' }).row).toEqual([])
  })
})

describe('DeepSeek 官方峰谷预设', () => {
  test('只填结构：七天全选、两个时段、不写死价格', () => {
    const preset = buildDeepSeekPeakPreset()
    expect(preset).toHaveLength(2)
    expect(preset[0]).toMatchObject({ start: '00:30', end: '08:30' })
    expect(preset[1]).toMatchObject({ start: '08:30', end: '00:30' })
    for (const entry of preset) {
      expect(entry.days).toEqual([1, 2, 3, 4, 5, 6, 7])
      expect(entry.input).toBeUndefined()
      expect(entry.output).toBeUndefined()
      expect(entry.cache_read).toBeUndefined()
      expect(entry.cache_write).toBeUndefined()
    }
  })
})

describe('落库序列化', () => {
  test('旧单层价原样输出，不生成 bands/timezone', () => {
    const settings = normalizePricesForSetting([
      { model: ' gpt-4o ', input: 2.5 },
    ])
    expect(settings).toEqual([
      {
        model: 'gpt-4o',
        input: 2.5,
        output: 0,
        cache_read: 0,
        cache_write: 0,
      },
    ])
    expect(settings[0]).not.toHaveProperty('bands')
    expect(settings[0]).not.toHaveProperty('timezone')
  })

  test('峰谷行保留 timezone 与显式 band 价（留空不冒充 0）', () => {
    const settings = normalizePricesForSetting([
      {
        model: 'gpt-4o',
        timezone: ' Asia/Shanghai ',
        bands: [
          {
            days: [3, 1, 1],
            start: ' 00:30 ',
            end: '08:30',
            input: 0,
            cache_write: 1.5,
          },
        ],
      },
    ])
    expect(settings).toEqual([
      {
        model: 'gpt-4o',
        input: 0,
        output: 0,
        cache_read: 0,
        cache_write: 0,
        timezone: 'Asia/Shanghai',
        bands: [
          {
            days: [1, 3],
            start: '00:30',
            end: '08:30',
            input: 0,
            cache_write: 1.5,
          },
        ],
      },
    ])
  })

  test('全空条目丢弃；仅有 bands 的行保留', () => {
    expect(normalizePricesForSetting([{ model: 'empty' }])).toEqual([])
    expect(
      normalizePricesForSetting([
        { model: 'band-only', bands: [band({ days: [7] })] },
      ])
    ).toHaveLength(1)
  })

  test('默认时区常量与后端一致', () => {
    expect(DEFAULT_PRICE_TIMEZONE).toBe('Asia/Shanghai')
  })
})
