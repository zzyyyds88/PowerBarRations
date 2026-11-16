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
import {
  PRICE_FIELDS,
  isValidPriceTimezone,
  parseClockHHMM,
  type PriceBand,
  type PricedChannelModel,
} from './channel-prices'

// 渠道价格「批量粘贴导入」的解析（ui-spec §9）：
//
// - JSON：价格行数组，形状同 `pbr_prices`（model + 四个单价 + 可选
//   timezone/bands）；
// - CSV：固定列序
//   `model,input,output,cache_read,cache_write,timezone,days,start,end`。
//   一行 = 一个峰谷时段（days/start/end 非空）或该模型的全天兜底行
//   （三列全空）。同一模型的多个 CSV 行合并为一条价格行，顺序即 band 顺序。
//
// 解析只负责结构与类型：**结构非法一律回报行号与原因，不做静默修复**；
// 时间与 days 的语义校验同样在此完成，后端仍是最终权威（`400
// invalid_prices`）。

export type PriceImportIssueCode =
  | 'invalid_json'
  | 'not_array'
  | 'invalid_row'
  | 'empty_model'
  | 'csv_columns'
  | 'empty_row'
  | 'duplicate_fallback'
  | 'duplicate_model'
  | 'too_many_bands'
  | 'invalid_timezone'
  | 'days_empty'
  | 'days_invalid'
  | 'time_invalid'
  | 'time_equal'
  | 'negative_price'

export interface PriceImportIssue {
  /** CSV 行号或 JSON 条目序号（均从 1 开始）；语法错误固定为 1。 */
  line: number
  code: PriceImportIssueCode
  detail?: string
}

export interface PriceImportResult {
  format: 'json' | 'csv'
  rows: PricedChannelModel[]
  issues: PriceImportIssue[]
}

/** 空/NaN 返回 undefined；非数字返回 null（由调用方记为 invalid_row）。 */
function parseNumberCell(raw: unknown): number | undefined | null {
  if (raw === undefined || raw === null) return undefined
  if (typeof raw === 'string' && raw.trim() === '') return undefined
  let value = Number.NaN
  if (typeof raw === 'number') value = raw
  else if (typeof raw === 'string') value = Number(raw.trim())
  return Number.isFinite(value) ? value : null
}

function readStringCell(raw: unknown): string | null {
  if (typeof raw !== 'string') return null
  const trimmed = raw.trim()
  return trimmed === '' ? null : trimmed
}

/** 把 `1-5` / `1;3;5` / `1,2,3` 解析为升序去重数组；非法返回 null。 */
export function parseWeekdayList(value: string): number[] | null {
  const parts = value.split(/[;,|\s]+/).filter((part) => part !== '')
  if (parts.length === 0) return null
  const days = new Set<number>()
  for (const part of parts) {
    const range = /^([1-7])-([1-7])$/.exec(part)
    if (range) {
      const from = Number(range[1])
      const to = Number(range[2])
      if (from > to) return null
      for (let day = from; day <= to; day += 1) days.add(day)
      continue
    }
    if (!/^[1-7]$/.test(part)) return null
    days.add(Number(part))
  }
  return [...days].sort((a, b) => a - b)
}

/** 周视图/预览用的紧凑写法：`1-5,7`。 */
export function formatWeekdayList(days: number[]): string {
  const sorted = [...new Set(days)].sort((a, b) => a - b)
  const parts: string[] = []
  let cursor = 0
  while (cursor < sorted.length) {
    const start = sorted[cursor]
    let end = start
    while (cursor + 1 < sorted.length && sorted[cursor + 1] === end + 1) {
      cursor += 1
      end = sorted[cursor]
    }
    parts.push(start === end ? String(start) : `${start}-${end}`)
    cursor += 1
  }
  return parts.join(',')
}

function issueCollector(): {
  issues: PriceImportIssue[]
  add: (line: number, code: PriceImportIssueCode, detail?: string) => void
} {
  const issues: PriceImportIssue[] = []
  const seen = new Set<string>()
  return {
    issues,
    add: (line, code, detail) => {
      const key = `${line}:${code}`
      if (seen.has(key)) return
      seen.add(key)
      issues.push({ line, code, ...(detail ? { detail } : {}) })
    },
  }
}

function buildBand(
  days: number[],
  start: string,
  end: string,
  prices: Partial<Record<(typeof PRICE_FIELDS)[number], number>>
): PriceBand {
  const band: PriceBand = { days, start, end }
  for (const field of PRICE_FIELDS) {
    const value = prices[field]
    if (value !== undefined) band[field] = value
  }
  return band
}

/** 用与后端一致的结构校验把 band 问题翻译成导入问题码。 */
function collectBandIssues(
  band: PriceBand,
  line: number,
  add: (line: number, code: PriceImportIssueCode, detail?: string) => void
): void {
  if (band.days.length === 0) add(line, 'days_empty')
  else if (band.days.some((day) => day < 1 || day > 7)) {
    add(line, 'days_invalid')
  }
  const start = parseClockHHMM(band.start)
  const end = parseClockHHMM(band.end)
  if (start === null || end === null) add(line, 'time_invalid')
  else if (start === end) add(line, 'time_equal')
  if (
    PRICE_FIELDS.some((field) => band[field] !== undefined && band[field] < 0)
  ) {
    add(line, 'negative_price')
  }
}

function parseJsonBand(
  raw: unknown,
  line: number,
  add: (line: number, code: PriceImportIssueCode, detail?: string) => void
): PriceBand | null {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
    add(line, 'invalid_row')
    return null
  }
  const record = raw as Record<string, unknown>
  const daysRaw = record.days
  let days: number[] = []
  if (!Array.isArray(daysRaw)) {
    add(line, 'days_invalid')
  } else {
    const parsed: number[] = []
    for (const item of daysRaw) {
      const day = parseNumberCell(item)
      if (day === null || day === undefined || !Number.isInteger(day)) {
        add(line, 'days_invalid')
        parsed.length = 0
        break
      }
      if (!parsed.includes(day)) parsed.push(day)
    }
    days = parsed.sort((a, b) => a - b)
  }
  const start = readStringCell(record.start)
  const end = readStringCell(record.end)
  if (start === null || end === null) add(line, 'time_invalid')
  const prices: Partial<Record<(typeof PRICE_FIELDS)[number], number>> = {}
  for (const field of PRICE_FIELDS) {
    const value = parseNumberCell(record[field])
    if (value === null) add(line, 'invalid_row')
    else if (value !== undefined) {
      if (value < 0) add(line, 'negative_price')
      prices[field] = value
    }
  }
  const band = buildBand(days, start ?? '', end ?? '', prices)
  collectBandIssues(band, line, add)
  return band
}

function parseJsonRow(
  raw: unknown,
  line: number,
  add: (line: number, code: PriceImportIssueCode, detail?: string) => void
): PricedChannelModel | null {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
    add(line, 'invalid_row')
    return null
  }
  const record = raw as Record<string, unknown>
  const model = readStringCell(record.model)
  if (model === null) {
    add(line, 'empty_model')
    return null
  }
  const row: PricedChannelModel = { model }
  for (const field of PRICE_FIELDS) {
    const value = parseNumberCell(record[field])
    if (value === null) add(line, 'invalid_row')
    else if (value !== undefined) {
      if (value < 0) add(line, 'negative_price')
      row[field] = value
    }
  }
  if (record.timezone !== undefined && record.timezone !== null) {
    const timezone = readStringCell(record.timezone)
    if (timezone === null) {
      add(line, 'invalid_row')
    } else {
      row.timezone = timezone
      if (!isValidPriceTimezone(timezone)) add(line, 'invalid_timezone')
    }
  }
  if (record.bands !== undefined && record.bands !== null) {
    if (!Array.isArray(record.bands)) {
      add(line, 'invalid_row')
    } else {
      const bands: PriceBand[] = []
      for (const bandRaw of record.bands) {
        const band = parseJsonBand(bandRaw, line, add)
        if (band) bands.push(band)
      }
      row.bands = bands
    }
  }
  return row
}

function parseJson(text: string): Pick<PriceImportResult, 'rows' | 'issues'> {
  const { issues, add } = issueCollector()
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    add(1, 'invalid_json')
    return { rows: [], issues }
  }
  if (!Array.isArray(parsed)) {
    add(1, 'not_array')
    return { rows: [], issues }
  }
  const rows: PricedChannelModel[] = []
  const models = new Set<string>()
  parsed.forEach((entry, index) => {
    const line = index + 1
    const row = parseJsonRow(entry, line, add)
    if (!row) return
    if (models.has(row.model)) {
      add(line, 'duplicate_model', row.model)
      return
    }
    if ((row.bands?.length ?? 0) > 64) add(line, 'too_many_bands')
    models.add(row.model)
    rows.push(row)
  })
  return { rows, issues }
}

function splitCsvLine(line: string): string[] {
  const cells: string[] = []
  let current = ''
  let quoted = false
  for (let index = 0; index < line.length; index += 1) {
    const char = line[index]
    if (quoted) {
      if (char === '"') {
        if (line[index + 1] === '"') {
          current += '"'
          index += 1
        } else {
          quoted = false
        }
      } else {
        current += char
      }
      continue
    }
    if (char === '"') {
      quoted = true
      continue
    }
    if (char === ',') {
      cells.push(current.trim())
      current = ''
      continue
    }
    current += char
  }
  cells.push(current.trim())
  return cells
}

function isCsvHeader(cells: string[]): boolean {
  const first = cells[0]?.toLowerCase()
  if (first !== 'model') return false
  return cells.some((cell) => cell.toLowerCase() === 'cache_read')
}

function hasRowPrice(row: PricedChannelModel): boolean {
  return PRICE_FIELDS.some((field) => row[field] !== undefined)
}

function parseCsv(text: string): Pick<PriceImportResult, 'rows' | 'issues'> {
  const { issues, add } = issueCollector()
  const rowsByModel = new Map<string, PricedChannelModel>()
  const lines = text.replaceAll(/\r\n?/g, '\n').split('\n')
  lines.forEach((raw, index) => {
    const line = index + 1
    const trimmed = raw.trim()
    if (trimmed === '' || trimmed.startsWith('#')) return
    const cells = splitCsvLine(trimmed)
    if (isCsvHeader(cells)) return
    if (cells.length < 5) {
      add(line, 'csv_columns')
      return
    }
    if (cells.length > 9) {
      add(line, 'csv_columns')
      return
    }
    while (cells.length < 9) cells.push('')

    const model = cells[0]
    if (model === '') {
      add(line, 'empty_model')
      return
    }
    const prices: Partial<Record<(typeof PRICE_FIELDS)[number], number>> = {}
    let priceInvalid = false
    PRICE_FIELDS.forEach((field, fieldIndex) => {
      const value = parseNumberCell(cells[fieldIndex + 1])
      if (value === null) {
        priceInvalid = true
        return
      }
      if (value === undefined) return
      if (value < 0) add(line, 'negative_price')
      prices[field] = value
    })
    if (priceInvalid) {
      add(line, 'invalid_row')
      return
    }

    const timezone = cells[5]
    if (timezone !== '' && !isValidPriceTimezone(timezone)) {
      add(line, 'invalid_timezone')
    }

    let row = rowsByModel.get(model)
    if (!row) {
      row = { model }
      rowsByModel.set(model, row)
    }

    const daysCell = cells[6]
    const startCell = cells[7]
    const endCell = cells[8]
    if (daysCell === '' && startCell === '' && endCell === '') {
      if (Object.keys(prices).length === 0) {
        add(line, 'empty_row')
        return
      }
      if (hasRowPrice(row)) {
        add(line, 'duplicate_fallback', model)
        return
      }
      Object.assign(row, prices)
      if (timezone !== '') row.timezone = timezone
      return
    }

    const days = parseWeekdayList(daysCell)
    if (days === null) {
      add(line, 'days_invalid')
      return
    }
    const band = buildBand(days, startCell, endCell, prices)
    collectBandIssues(band, line, add)
    if (timezone !== '') row.timezone = timezone
    row.bands = [...(row.bands ?? []), band]
    if (row.bands.length > 64) add(line, 'too_many_bands')
  })
  return { rows: [...rowsByModel.values()], issues }
}

function hasImportableContent(row: PricedChannelModel): boolean {
  return hasRowPrice(row) || (row.bands?.length ?? 0) > 0
}

/**
 * 解析批量粘贴文本：以 `[`/`{` 开头按 JSON，否则按 CSV。CSV 行号从 1 开始
 * （含表头行），JSON 条目序号从 1 开始。
 */
export function parsePriceImport(text: string): PriceImportResult {
  const trimmed = text.trim()
  const format =
    trimmed.startsWith('[') || trimmed.startsWith('{') ? 'json' : 'csv'
  if (trimmed === '') return { format, rows: [], issues: [] }
  const parsed = format === 'json' ? parseJson(trimmed) : parseCsv(trimmed)
  return {
    format,
    rows: parsed.rows.filter(hasImportableContent),
    issues: parsed.issues,
  }
}

/**
 * 把导入结果合并回表单：同模型的行整体替换（保留其原位置），新模型追加在
 * 末尾；未出现在导入内容里的行保持不动。
 */
export function mergeImportedPrices(
  current: ChannelModelPrice[],
  imported: PricedChannelModel[]
): PricedChannelModel[] {
  const next: PricedChannelModel[] = current.map((row) => ({ ...row }))
  for (const row of imported) {
    const index = next.findIndex((item) => item.model === row.model)
    if (index >= 0) next[index] = row
    else next.push(row)
  }
  return next
}
