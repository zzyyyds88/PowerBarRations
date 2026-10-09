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
/*
渠道状态 API 客户端对 api-spec §5.3 / §6.12 的消费契约：

- 单渠道详情 `GET /api/channels/{name}/status?window=`；
- 全量汇总 `GET /api/channels/status?window=`（形状不含 daily/recent_failures/probe_history）；
- 探活 `POST /api/channels/{name}/test`（每次调用落一条探活记录）；
- 成功一律读**裸对象**（api-spec §2.3），失败交给 axios 拒绝。
*/
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  getChannelStatus,
  listChannelStatus,
  probeChannelStatus,
} from '../status-api'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
  getFreshAuthHeaders: vi.fn(async () => ({})),
}))

const mockedGet = vi.mocked(api.get)
const mockedPost = vi.mocked(api.post)

const DETAIL = {
  channel: {
    name: 'channel-a',
    enabled: true,
    type: 'openai',
    base_url: 'https://vendor.example/v1',
    key_set: true,
    key_prefix: 'sk-a',
    models: ['model-1'],
    model_mapping: {},
  },
  reference: { lanes: [] },
  runtime: { member_count: 0, available_count: 0, members: [] },
  window: '24h',
  summary: {
    requests: 120,
    successes: 118,
    failures: 2,
    success_rate: 0.9833,
    avg_total_ms: 840,
    avg_ttft_ms: 220,
    estimated_cost: 0.12,
  },
  models: [
    {
      model: 'model-1',
      requests: 120,
      successes: 118,
      failures: 2,
      success_rate: 0.9833,
      avg_total_ms: 840,
      avg_ttft_ms: 220,
      output_tps: 41.2,
      tokens_out: 56000,
    },
  ],
  daily: [],
  recent_failures: [],
  probe_history: [],
  upstream_updates: {
    last_check_ts: null,
    pending_add: [],
    pending_remove: [],
  },
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('getChannelStatus', () => {
  it('reads the bare detail object from the windowed channel status endpoint', async () => {
    mockedGet.mockResolvedValue({ data: DETAIL } as never)

    const detail = await getChannelStatus('channel-a', '24h')

    expect(mockedGet).toHaveBeenCalledTimes(1)
    const [url, config] = mockedGet.mock.calls[0] ?? []
    expect(url).toBe('/api/channels/channel-a/status')
    expect(config?.params).toEqual({ window: '24h' })
    // 裸对象：不加信封解包，契约字段原样透出。
    expect(detail.summary.success_rate).toBe(0.9833)
    // 模型状态表（渠道状态页的主内容）随详情一起透出，字段口径原样。
    expect(detail.models[0]?.output_tps).toBe(41.2)
    expect(detail.models[0]?.tokens_out).toBe(56000)
    expect(detail.runtime.members).toEqual([])
    expect(detail.upstream_updates.last_check_ts).toBeNull()
  })

  it('forwards the 7d window and escapes channel names in the path', async () => {
    mockedGet.mockResolvedValue({ data: DETAIL } as never)

    await getChannelStatus('vendor/model', '7d')

    const [url, config] = mockedGet.mock.calls[0] ?? []
    expect(url).toBe('/api/channels/vendor%2Fmodel/status')
    expect(config?.params).toEqual({ window: '7d' })
  })
})

describe('listChannelStatus', () => {
  it('reads the aggregate items for the requested window', async () => {
    mockedGet.mockResolvedValue({
      data: {
        window: '7d',
        items: [
          {
            channel: 'channel-a',
            enabled: true,
            type: 'openai',
            lane_count: 1,
            member_count: 1,
            available_count: 1,
            degraded: false,
            requests: 120,
            failures: 2,
            success_rate: 0.9833,
            estimated_cost: 0.12,
            last_error_kind: '',
          },
        ],
      },
    } as never)

    const result = await listChannelStatus('7d')

    const [url, config] = mockedGet.mock.calls[0] ?? []
    expect(url).toBe('/api/channels/status')
    expect(config?.params).toEqual({ window: '7d' })
    expect(result.items[0]?.lane_count).toBe(1)
    // 汇总形状里没有 daily / recent_failures / probe_history。
    expect(result.items[0]).not.toHaveProperty('probe_history')
  })
})

describe('probeChannelStatus', () => {
  it('posts to the stable probe endpoint and leaves error presentation to the caller', async () => {
    mockedPost.mockResolvedValue({
      data: {
        channel: 'channel-a',
        upstream_model: 'model-1',
        status: 'success',
        duration_ms: 320,
        ok: true,
        status_code: 200,
        latency_ms: 320,
      },
    } as never)

    const result = await probeChannelStatus('channel-a')

    const [url, body, config] = mockedPost.mock.calls[0] ?? []
    expect(url).toBe('/api/channels/channel-a/test')
    expect(body).toBeUndefined()
    expect(config?.skipErrorHandler).toBe(true)
    expect(result.ok).toBe(true)
  })
})
