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

// 全 locale 统一 K/M/B 缩写（2026-10-25 用户裁决：中文界面不再用 亿/万，
// 大数一律 K/M/B 短符号；zhCN/zhTW 非标准 tag 仍不得抛异常）。
describe('formatTokenAmount', () => {
  it('中文：K / M / B 数量级（与其它语言一致）', () => {
    expect(formatTokenAmount(100_000_000, 'zh')).toBe('100.0M')
    expect(formatTokenAmount(1_510_000_000, 'zh')).toBe('1.5B')
    expect(formatTokenAmount(556_400_000, 'zh-CN')).toBe('556.4M')
    expect(formatTokenAmount(15_000, 'zh')).toBe('15.0K')
    expect(formatTokenAmount(9_999, 'zh')).toBe('10.0K')
  })

  it('英文：B / M / K 数量级', () => {
    expect(formatTokenAmount(100_000_000, 'en')).toBe('100.0M')
    expect(formatTokenAmount(1_510_000_000, 'en')).toBe('1.5B')
    expect(formatTokenAmount(15_000, 'en')).toBe('15.0K')
    expect(formatTokenAmount(999, 'en')).toBe('999')
  })

  it('项目自己的非标准 tag（zhCN / zhTW）不得抛异常——曾把整页打成 500', () => {
    expect(() => formatTokenAmount(286_900_000, 'zhCN')).not.toThrow()
    expect(formatTokenAmount(286_900_000, 'zhCN')).toBe('286.9M')
    expect(formatTokenAmount(286_900_000, 'zhTW')).toBe('286.9M')
    expect(formatTokenAmount(15_000, 'zhCN')).toBe('15.0K')
    // 小数量走 Intl 千分位：此处最容易踩 `Invalid language tag`
    expect(formatTokenAmount(9_999, 'zhCN')).toBe('10.0K')
    // 未知语言不得抛，回落 Intl 默认
    expect(() => formatTokenAmount(1_000_000, 'xx-YY')).not.toThrow()
  })

  it('缺省语言与边界值', () => {
    expect(formatTokenAmount(1_510_000_000)).toBe('1.5B')
    expect(formatTokenAmount(0, 'zh')).toBe('0')
    expect(formatTokenAmount(-5, 'zh')).toBe('0')
    expect(formatTokenAmount(Number.NaN, 'zh')).toBe('0')
  })
})
