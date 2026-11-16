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
import { renderHook } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { useCommonLogsColumns } from '../columns/common-logs-columns'

function columnIds(isAdmin: boolean): string[] {
  const { result } = renderHook(() => useCommonLogsColumns(isAdmin, false))
  return result.current.map((column) => {
    if (column.id) return String(column.id)
    if ('accessorKey' in column && column.accessorKey) {
      return String(column.accessorKey)
    }
    return ''
  })
}

describe('common logs column shape', () => {
  // ui-spec §6.6：保留上游详细列形态（时间/类型/渠道/令牌/模型/流/输入输出/花费/耗时/详情）。
  test('exposes the localized PBR metadata columns', () => {
    const ids = columnIds(true)
    for (const expected of [
      'created_at',
      'channel',
      'token_name',
      'model_name',
      'is_stream',
      'prompt_tokens',
      'quota',
      'use_time',
      'content',
    ]) {
      expect(ids).toContain(expected)
    }
  })

  // PBR 无分组/订阅/多用户语义：这些上游列整列消失，而不是渲染空值。
  // 用户列按 ui-spec §6.6「PBR 单用户不设用户列」移除（消费方身份由令牌列表达）。
  test('omits group, subscription and user columns', () => {
    const ids = columnIds(true)
    for (const removed of ['group', 'subscription', 'user']) {
      expect(ids).not.toContain(removed)
    }
  })

  test('hides the admin channel column from the self view', () => {
    expect(columnIds(false)).not.toContain('channel')
    expect(columnIds(false)).not.toContain('user')
  })
})
