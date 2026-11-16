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
import { AxiosError, type AxiosAdapter } from 'axios'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getChannels } from '@/features/channels/api'
import { getModels } from '@/features/models/api'
import { getAuditLogs } from '@/features/usage-logs/audit/api'
import { listPBRRequestLogs } from '@/features/usage-logs/pbr/pbr-logs-api'
import { api } from '@/lib/api'
import { getServerErrorDetails } from '@/lib/server-error-message'

// 回归：管理面统一信封（api-spec §2.3/§3）下，列表响应是裸 `{items,...}`，
// 稳定面 cursor 列表是 `{items,next_cursor}`；失败是 §3 错误包络 + 真实状态码。

const originalAdapter = api.defaults.adapter

afterEach(() => {
  api.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
})

function stubAdapter(handler: AxiosAdapter): void {
  api.defaults.adapter = vi.fn<AxiosAdapter>().mockImplementation(handler)
}

describe('list envelope migration', () => {
  it('reads the base-surface channel list from a bare items/total payload', async () => {
    stubAdapter(async (config) => ({
      data: {
        items: [{ id: 1, name: 'channel-a' }],
        total: 1,
        page: 1,
        page_size: 20,
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }))

    const result = await getChannels({ p: 1, page_size: 20 })

    expect(result.items).toHaveLength(1)
    expect(result.total).toBe(1)
  })

  it('reads the model catalog list from a bare items/total payload', async () => {
    stubAdapter(async (config) => ({
      data: { items: [{ id: 7, model_name: 'm' }], total: 1 },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }))

    const result = await getModels()

    expect(result.items[0]?.id).toBe(7)
    expect(result.total).toBe(1)
  })

  it('reads the audit list from a bare items/total payload', async () => {
    stubAdapter(async (config) => ({
      data: { items: [{ event_id: 'e1' }], total: 3 },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }))

    const result = await getAuditLogs('self', { p: 1, page_size: 20 })

    expect(result.items).toHaveLength(1)
    expect(result.total).toBe(3)
  })

  it('parses the stable cursor list {items,next_cursor}', async () => {
    stubAdapter(async (config) => ({
      data: {
        items: [{ id: 1, attempts: [] }],
        next_cursor: 'cursor-2',
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }))

    const result = await listPBRRequestLogs({ limit: 1 })

    expect(result.items).toHaveLength(1)
    expect(result.next_cursor).toBe('cursor-2')
  })

  it('normalizes a missing next_cursor to null so paging terminates', async () => {
    stubAdapter(async (config) => ({
      data: { items: [] },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }))

    const result = await listPBRRequestLogs()

    expect(result.next_cursor).toBeNull()
  })
})

describe('error envelope migration', () => {
  it('exposes the 404 code/hint from a rejected list request', async () => {
    stubAdapter(async (config) => {
      throw new AxiosError('Not Found', 'ERR_BAD_REQUEST', config, undefined, {
        data: {
          error: {
            code: 'channel_not_found',
            message: "channel 'ghost' not found",
            hint: 'GET /api/v1/channels',
          },
        },
        status: 404,
        statusText: 'Not Found',
        headers: {},
        config,
      })
    })

    await expect(getChannels()).rejects.toBeInstanceOf(AxiosError)
    try {
      await getChannels()
    } catch (error) {
      const details = getServerErrorDetails(error)
      expect(details.code).toBe('channel_not_found')
      expect(details.hint).toBe('GET /api/v1/channels')
      expect(details.message).toBe("channel 'ghost' not found")
    }
  })
})
