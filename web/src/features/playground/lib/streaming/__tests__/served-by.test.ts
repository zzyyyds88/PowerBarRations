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
  normalizeServedBy,
  servedByFromResponse,
  servedByFromStreamHeaders,
} from '../served-by'

describe('X-Served-By 读取（ui-spec §6.8）', () => {
  test('非流式：从 fetch Response 读取头值', () => {
    const response = new Response('{}', {
      headers: { 'X-Served-By': 'channel=1:channel-a, model=model-x' },
    })
    expect(servedByFromResponse(response)).toBe(
      'channel=1:channel-a, model=model-x'
    )
  })

  test('非流式：头缺失时返回 undefined', () => {
    expect(servedByFromResponse(new Response('{}'))).toBeUndefined()
  })

  test('非流式：头为空串视作缺失', () => {
    const response = new Response('{}', { headers: { 'X-Served-By': '   ' } })
    expect(servedByFromResponse(response)).toBeUndefined()
  })

  test('流式：sse.js 的 open 事件 headers 是小写键 + 值数组', () => {
    expect(
      servedByFromStreamHeaders({
        'x-served-by': ['channel=2:channel-b, model=model-y'],
      })
    ).toBe('channel=2:channel-b, model=model-y')
  })

  test('流式：兼容值直接是字符串的形态', () => {
    expect(
      servedByFromStreamHeaders({ 'x-served-by': 'channel=1:channel-a' })
    ).toBe('channel=1:channel-a')
  })

  test('流式：无 headers 或键缺失时返回 undefined', () => {
    expect(servedByFromStreamHeaders(undefined)).toBeUndefined()
    expect(
      servedByFromStreamHeaders({ 'content-type': ['text/event-stream'] })
    ).toBeUndefined()
  })

  test('normalizeServedBy 去空白且空串为 undefined', () => {
    expect(normalizeServedBy('  a  ')).toBe('a')
    expect(normalizeServedBy('')).toBeUndefined()
    expect(normalizeServedBy(null)).toBeUndefined()
    expect(normalizeServedBy(undefined)).toBeUndefined()
  })
})
