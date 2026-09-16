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

import { savePBRFailover } from '../pbr-routing-api'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    put: vi.fn(),
  },
}))

const mockedGet = vi.mocked(api.get)
const mockedPut = vi.mocked(api.put)

describe('savePBRFailover 六键来源', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  test('新车道用系统设置里的默认六键（不写死前端默认值）', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/system/options') {
        return {
          data: {
            lane_defaults: {
              member_max_attempts: 5,
              member_retry_interval_seconds: 0,
              member_non_stream_response_timeout_seconds: 90,
              member_stream_first_event_timeout_seconds: 15,
              member_cooldown_seconds: 30,
              member_affinity_seconds: 7,
            },
          },
        } as never
      }
      // 车道不存在 → 404 风格的失败
      throw new Error('not found')
    })
    mockedPut.mockResolvedValue({ data: {} } as never)

    await savePBRFailover('model-1', [{ channel: 'channel-a', priority: 10 }])

    expect(mockedPut).toHaveBeenCalledWith('/api/v1/lanes/model-1', {
      enabled: true,
      mode: 'failover',
      config: {
        member_max_attempts: 5,
        member_retry_interval_seconds: 0,
        member_non_stream_response_timeout_seconds: 90,
        member_stream_first_event_timeout_seconds: 15,
        member_cooldown_seconds: 30,
        member_affinity_seconds: 7,
      },
      members: [{ channel: 'channel-a', priority: 10 }],
    })
  })

  test('已有车道保留其自身六键，只改成员顺序', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/system/options') {
        return { data: { lane_defaults: { member_max_attempts: 5 } } } as never
      }
      return {
        data: {
          config: {
            member_max_attempts: 1,
            member_retry_interval_seconds: 0,
            member_non_stream_response_timeout_seconds: 120,
            member_stream_first_event_timeout_seconds: 30,
            member_cooldown_seconds: 60,
            member_affinity_seconds: 0,
          },
        },
      } as never
    })
    mockedPut.mockResolvedValue({ data: {} } as never)

    await savePBRFailover('model-1', [{ channel: 'channel-b', priority: 5 }])

    const [, body] = mockedPut.mock.calls[0] as [string, { config: unknown }]
    expect(body.config).toMatchObject({ member_max_attempts: 1 })
  })

  test('系统设置读取失败时回落内置默认值', async () => {
    mockedGet.mockRejectedValue(new Error('boom'))
    mockedPut.mockResolvedValue({ data: {} } as never)

    await savePBRFailover('model-1', [{ channel: 'channel-a', priority: 1 }])

    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { config: Record<string, number> },
    ]
    expect(body.config).toMatchObject({
      member_max_attempts: 2,
      member_retry_interval_seconds: 3,
      member_non_stream_response_timeout_seconds: 120,
      member_stream_first_event_timeout_seconds: 30,
      member_cooldown_seconds: 60,
      member_affinity_seconds: 0,
    })
  })
})
