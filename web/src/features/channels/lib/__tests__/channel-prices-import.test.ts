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
  formatWeekdayList,
  mergeImportedPrices,
  parsePriceImport,
  parseWeekdayList,
} from '../channel-prices-import'

// 批量粘贴导入（ui-spec §9）：CSV 固定列序
// `model,input,output,cache_read,cache_write,timezone,days,start,end`，
// 一行 = 一个时段或该模型的全天兜底行；JSON 为价格行数组。解析失败必须带
// 行号与原因，且不静默修复结构。

const CSV_HEADER =
  'model,input,output,cache_read,cache_write,timezone,days,start,end'

function codes(issues: { code: string }[]): string[] {
  return issues.map((issue) => issue.code)
}

describe('days 列表', () => {
  test('支持区间与分隔符', () => {
    expect(parseWeekdayList('1-5')).toEqual([1, 2, 3, 4, 5])
    expect(parseWeekdayList('1;3;5')).toEqual([1, 3, 5])
    expect(parseWeekdayList('1,2,7')).toEqual([1, 2, 7])
    expect(parseWeekdayList('7')).toEqual([7])
  })

  test('拒绝越界、倒序与非法文本', () => {
    for (const value of ['0', '8', '5-1', 'mon', '1-', '', '1--2']) {
      expect(parseWeekdayList(value)).toBeNull()
    }
  })

  test('紧凑格式化压缩连续区间', () => {
    expect(formatWeekdayList([1, 2, 3, 5])).toBe('1-3,5')
    expect(formatWeekdayList([7])).toBe('7')
    expect(formatWeekdayList([3, 1, 2])).toBe('1-3')
  })
})

describe('CSV 解析', () => {
  test('兜底行与时段行合并为一条价格行', () => {
    const result = parsePriceImport(
      [
        CSV_HEADER,
        'gpt-4o,10,20,,,Asia/Shanghai,,,',
        'gpt-4o,,,,,,1-5,00:30,08:30',
        'gpt-4o,,,,,,6-7,08:30,00:30',
      ].join('\n')
    )

    expect(result.format).toBe('csv')
    expect(result.issues).toEqual([])
    expect(result.rows).toEqual([
      {
        model: 'gpt-4o',
        input: 10,
        output: 20,
        timezone: 'Asia/Shanghai',
        bands: [
          { days: [1, 2, 3, 4, 5], start: '00:30', end: '08:30' },
          { days: [6, 7], start: '08:30', end: '00:30' },
        ],
      },
    ])
  })

  test('无表头也接受，空行与注释跳过', () => {
    const result = parsePriceImport(
      ['', '# comment', 'gpt-4o-mini,1,2,,,UTC,,,', ''].join('\n')
    )
    expect(result.issues).toEqual([])
    expect(result.rows).toEqual([
      { model: 'gpt-4o-mini', input: 1, output: 2, timezone: 'UTC' },
    ])
  })

  test('行号按源文件（含表头）报告', () => {
    const result = parsePriceImport(
      [CSV_HEADER, 'gpt-4o,10,20,,,UTC,1-5,9:00,18:00'].join('\n')
    )
    expect(result.issues).toEqual([{ line: 2, code: 'time_invalid' }])
  })

  test('空 days、相等时间、负价与空行都拒绝', () => {
    const result = parsePriceImport(
      [
        'a,1,2,,,UTC,,09:00,18:00',
        'b,1,2,,,UTC,1-5,10:00,10:00',
        'c,-1,2,,,UTC,1-5,09:00,18:00',
        'd,,,,,UTC,,,',
      ].join('\n')
    )
    expect(codes(result.issues)).toEqual(
      expect.arrayContaining([
        'days_invalid',
        'time_equal',
        'negative_price',
        'empty_row',
      ])
    )
    expect(result.issues.filter((issue) => issue.code === 'empty_row')).toEqual(
      [{ line: 4, code: 'empty_row' }]
    )
  })

  test('重复全天行与列数错误给出行号', () => {
    const result = parsePriceImport(
      [
        'a,1,2,,,UTC,,,',
        'a,3,4,,,UTC,,,',
        'b,1',
        'c,1,2,3,4,UTC,1-5,09:00,18:00,extra',
      ].join('\n')
    )
    expect(result.issues).toEqual(
      expect.arrayContaining([
        { line: 2, code: 'duplicate_fallback', detail: 'a' },
        { line: 3, code: 'csv_columns' },
        { line: 4, code: 'csv_columns' },
      ])
    )
  })

  test('Excel 导出的 CRLF 与引号单元格可解析', () => {
    const result = parsePriceImport(
      `${CSV_HEADER}\r\n"gpt-4o",10,20,,,UTC,"1-5","00:30","08:30"\r\n`
    )
    expect(result.issues).toEqual([])
    expect(result.rows[0].bands).toEqual([
      {
        days: [1, 2, 3, 4, 5],
        start: '00:30',
        end: '08:30',
        input: 10,
        output: 20,
      },
    ])
  })
})

describe('JSON 解析', () => {
  test('价格行数组含 timezone 与 bands', () => {
    const result = parsePriceImport(
      JSON.stringify([
        {
          model: 'gpt-4o',
          input: 10,
          output: 20,
          timezone: 'Asia/Shanghai',
          bands: [
            { days: [1, 2, 3, 4, 5], start: '00:30', end: '08:30', input: 5 },
          ],
        },
        { model: 'gpt-4o-mini', input: 1 },
      ])
    )

    expect(result.format).toBe('json')
    expect(result.issues).toEqual([])
    expect(result.rows).toEqual([
      {
        model: 'gpt-4o',
        input: 10,
        output: 20,
        timezone: 'Asia/Shanghai',
        bands: [
          { days: [1, 2, 3, 4, 5], start: '00:30', end: '08:30', input: 5 },
        ],
      },
      { model: 'gpt-4o-mini', input: 1 },
    ])
  })

  test('语法错误、非数组与重复模型', () => {
    expect(codes(parsePriceImport('{').issues)).toEqual(['invalid_json'])
    expect(codes(parsePriceImport('{}').issues)).toEqual(['not_array'])
    expect(
      parsePriceImport(
        JSON.stringify([
          { model: 'a', input: 1 },
          { model: 'a', input: 2 },
        ])
      ).issues
    ).toEqual([{ line: 2, code: 'duplicate_model', detail: 'a' }])
  })

  test('band 结构问题逐条目报告', () => {
    const result = parsePriceImport(
      JSON.stringify([
        {
          model: 'a',
          bands: [
            { days: [], start: '09:00', end: '09:00' },
            { days: [1], start: '9:00', end: '18:00' },
          ],
        },
      ])
    )
    expect(codes(result.issues)).toEqual(
      expect.arrayContaining(['days_empty', 'time_equal', 'time_invalid'])
    )
    expect(result.issues.every((issue) => issue.line === 1)).toBe(true)
  })

  test('空文本没有行也没有问题', () => {
    expect(parsePriceImport('   ')).toEqual({
      format: 'csv',
      rows: [],
      issues: [],
    })
  })
})

describe('合并回表单', () => {
  test('同模型替换原位置，新模型追加，其余不动', () => {
    const merged = mergeImportedPrices(
      [
        { model: 'a', input: 1 },
        { model: 'b', input: 2 },
      ],
      [
        { model: 'b', input: 20, bands: [] },
        { model: 'c', input: 3 },
      ]
    )
    expect(merged).toEqual([
      { model: 'a', input: 1 },
      { model: 'b', input: 20, bands: [] },
      { model: 'c', input: 3 },
    ])
  })
})
