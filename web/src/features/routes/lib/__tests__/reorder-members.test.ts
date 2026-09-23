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

import { priorityForIndex, reorderMembers } from '../reorder-members'

// 成员链顺序即故障切换顺序（ADR 0006、routing-spec §1.2），所以拖拽重排是
// 纯数组变换：边界用例按 test-spec §3.1 逐条覆盖（首位/末位/相邻/自身/未知 id）。
interface Row {
  id: string
}

const rows: Row[] = [{ id: 'a' }, { id: 'b' }, { id: 'c' }]
const ids = (list: Row[]) => list.map((row) => row.id)

describe('reorderMembers', () => {
  it('moves a row to the first position', () => {
    expect(ids(reorderMembers(rows, 'c', 'a', 'before'))).toEqual([
      'c',
      'a',
      'b',
    ])
  })

  it('moves a row to the last position', () => {
    expect(ids(reorderMembers(rows, 'a', 'c', 'after'))).toEqual([
      'b',
      'c',
      'a',
    ])
  })

  it('swaps adjacent rows for both drop positions', () => {
    // b 落到 a 之前 / 之后（相邻交换的两个方向都要正确，否则拖拽会"吞掉"一行）。
    expect(ids(reorderMembers(rows, 'b', 'a', 'before'))).toEqual([
      'b',
      'a',
      'c',
    ])
    expect(ids(reorderMembers(rows, 'b', 'a', 'after'))).toEqual([
      'a',
      'b',
      'c',
    ])
  })

  it('returns the same array reference when dropping onto itself', () => {
    const result = reorderMembers(rows, 'b', 'b')
    expect(result).toBe(rows)
  })

  it('returns the same array reference for unknown ids or blank input', () => {
    // 未知 id 必须原样返回：调用方靠引用相等跳过无意义的置脏与 setState。
    expect(reorderMembers(rows, 'ghost', 'a')).toBe(rows)
    expect(reorderMembers(rows, 'a', 'ghost')).toBe(rows)
    expect(reorderMembers(rows, '', 'a')).toBe(rows)
    expect(reorderMembers(rows, 'a', '')).toBe(rows)
  })

  it('does not mutate the input array', () => {
    const before = ids(rows)
    reorderMembers(rows, 'c', 'a', 'before')
    expect(ids(rows)).toEqual(before)
  })

  it('defaults to inserting before the target', () => {
    expect(ids(reorderMembers(rows, 'c', 'b'))).toEqual(['a', 'c', 'b'])
  })
})

describe('priorityForIndex', () => {
  // routing-spec §1.2：数字大者优先，保存时按数组下标生成。
  it('gives the first row the largest priority', () => {
    expect(priorityForIndex(0, 3)).toBe(3)
    expect(priorityForIndex(1, 3)).toBe(2)
    expect(priorityForIndex(2, 3)).toBe(1)
  })

  it('stays strictly descending so array order is the failover order', () => {
    const total = 5
    const priorities = Array.from({ length: total }, (_, i) =>
      priorityForIndex(i, total)
    )
    expect(priorities).toEqual([5, 4, 3, 2, 1])
    for (let i = 1; i < priorities.length; i += 1) {
      expect(priorities[i]).toBeLessThan(priorities[i - 1])
    }
  })
})
