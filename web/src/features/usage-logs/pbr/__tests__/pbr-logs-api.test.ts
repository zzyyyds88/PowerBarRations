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
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { listPBRRequestLogs } from '../pbr-logs-api'
import {
  attemptStatusClass,
  isSkipStatus,
  summarizeAttempts,
} from '../attempt-status'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn() },
}))

const mockedGet = vi.mocked(api.get)

describe('PBR 请求日志数据源', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  test('读 PBR /api/v1/logs 并把筛选映射成查询参数', async () => {
    mockedGet.mockResolvedValue({ data: { items: [], next_cursor: null } } as never)

    await listPBRRequestLogs({
      lane: 'model-1',
      success: false,
      limit: 50,
      cursor: 'abc',
    })

    expect(mockedGet).toHaveBeenCalledWith('/api/v1/logs', {
      params: {
        limit: 50,
        lane: 'model-1',
        success: 'false',
        cursor: 'abc',
      },
    })
  })

  test('未设置 success 时不发送该参数（默认全部）', async () => {
    mockedGet.mockResolvedValue({ data: { items: [] } } as never)

    await listPBRRequestLogs({ model: 'model-2' })

    const [, config] = mockedGet.mock.calls[0] as [
      string,
      { params: Record<string, unknown> },
    ]
    expect(config.params).not.toHaveProperty('success')
    expect(config.params.model).toBe('model-2')
  })
})

describe('attempts 状态展示', () => {
  test('跳过类状态有独立配色与原因语义', () => {
    expect(isSkipStatus('cooldown')).toBe(true)
    expect(isSkipStatus('circuit_break')).toBe(true)
    expect(isSkipStatus('skipped')).toBe(true)
    expect(isSkipStatus('failed')).toBe(false)
    expect(attemptStatusClass('success')).not.toBe(attemptStatusClass('failed'))
    expect(attemptStatusClass('unknown-status')).toBe(
      'bg-muted text-muted-foreground'
    )
  })

  test('列表列摘要把尝试链按顺序串起来', () => {
    expect(
      summarizeAttempts([
        { attempt_num: 1, member: 'a/m', status: 'failed', duration_ms: 10 },
        { attempt_num: 2, member: 'b/m', status: 'success', duration_ms: 20 },
      ])
    ).toBe('1.failed → 2.success')
    expect(summarizeAttempts([])).toBe('')
    expect(summarizeAttempts(undefined)).toBe('')
  })
})
