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

import { formatTokenAmount } from '@/lib/format'

// 中文界面用「亿 / 万」，其它语言保持 B / M / K（董事长 2026-10-10 口径）。
describe('formatTokenAmount', () => {
  it('中文：亿 / 万 数量级', () => {
    expect(formatTokenAmount(100_000_000, 'zh')).toBe('1.00 亿')
    expect(formatTokenAmount(1_510_000_000, 'zh')).toBe('15.10 亿')
    expect(formatTokenAmount(556_400_000, 'zh-CN')).toBe('5.56 亿')
    expect(formatTokenAmount(15_000, 'zh')).toBe('1.50 万')
    expect(formatTokenAmount(9_999, 'zh')).toBe('9,999')
  })

  it('英文：B / M / K 数量级', () => {
    expect(formatTokenAmount(100_000_000, 'en')).toBe('100.0M')
    expect(formatTokenAmount(1_510_000_000, 'en')).toBe('1.5B')
    expect(formatTokenAmount(15_000, 'en')).toBe('15.0K')
    expect(formatTokenAmount(999, 'en')).toBe('999')
  })

  it('缺省语言与边界值', () => {
    expect(formatTokenAmount(1_510_000_000)).toBe('1.5B')
    expect(formatTokenAmount(0, 'zh')).toBe('0')
    expect(formatTokenAmount(-5, 'zh')).toBe('0')
    expect(formatTokenAmount(Number.NaN, 'zh')).toBe('0')
  })
})
