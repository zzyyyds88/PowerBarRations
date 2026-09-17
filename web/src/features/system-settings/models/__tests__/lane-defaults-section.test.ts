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

import { fetchLaneDefaults } from '../lane-defaults'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), put: vi.fn() },
}))

const mockedGet = vi.mocked(api.get)

describe('fetchLaneDefaults', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  test('读 /api/v1/system/options 的 lane_defaults', async () => {
    mockedGet.mockResolvedValue({
      data: {
        lane_defaults: {
          member_max_attempts: 4,
          member_retry_interval_seconds: 1,
          member_non_stream_response_timeout_seconds: 60,
          member_stream_first_event_timeout_seconds: 20,
          member_cooldown_seconds: 45,
          member_affinity_seconds: 3,
        },
      },
    } as never)

    const defaults = await fetchLaneDefaults()

    expect(mockedGet).toHaveBeenCalledWith('/api/v1/system/options')
    expect(defaults.member_max_attempts).toBe(4)
    expect(defaults.member_affinity_seconds).toBe(3)
  })

  test('响应缺少 lane_defaults 时给出与后端一致的默认六键', async () => {
    mockedGet.mockResolvedValue({ data: {} } as never)

    const defaults = await fetchLaneDefaults()

    expect(defaults).toEqual({
      member_max_attempts: 2,
      member_retry_interval_seconds: 3,
      member_non_stream_response_timeout_seconds: 120,
      member_stream_first_event_timeout_seconds: 30,
      member_cooldown_seconds: 60,
      member_affinity_seconds: 0,
    })
  })
})
