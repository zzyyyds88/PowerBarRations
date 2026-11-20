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
import { cleanup, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { renderPage as renderConsolePage } from '@/test-utils/render-page'

import { ChannelSpeedPanel } from '..'

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
const URL = '/api/channel-speed'

const ITEMS = [
  {
    channel: 'channel-fast',
    enabled: true,
    requests: 150,
    successes: 148,
    failures: 2,
    success_rate: 0.9867,
    p50_total_ms: 620,
    p95_total_ms: 1800,
    avg_ttft_ms: 220,
    output_tps: 82.4,
    tps_samples: 120,
    output_tokens_per_second: 55.55555555555556,
    generation_tokens_per_second: 120.5,
    tokens_in: 10000,
    tokens_out: 50000,
    tokens_total: 60000,
    estimated_cost: 0.12,
  },
  {
    channel: 'channel-slow',
    enabled: true,
    requests: 40,
    successes: 40,
    failures: 0,
    success_rate: 1,
    p50_total_ms: 900,
    p95_total_ms: 2400,
    avg_ttft_ms: 350,
    output_tps: 21.2,
    tps_samples: 30,
    output_tokens_per_second: 8.88888888888889,
    generation_tokens_per_second: 44.2,
    tokens_in: 3000,
    tokens_out: 8000,
    tokens_total: 11000,
    estimated_cost: 0.04,
  },
  {
    channel: 'channel-idle',
    enabled: true,
    requests: 0,
    successes: 0,
    failures: 0,
    success_rate: 0,
    p50_total_ms: 0,
    p95_total_ms: 0,
    avg_ttft_ms: 0,
    output_tps: 0,
    tps_samples: 0,
    output_tokens_per_second: 0,
    generation_tokens_per_second: 0,
    tokens_in: 0,
    tokens_out: 0,
    tokens_total: 0,
    estimated_cost: 0,
  },
  {
    channel: 'channel-paused',
    enabled: false,
    requests: 0,
    successes: 0,
    failures: 0,
    success_rate: 0,
    p50_total_ms: 0,
    p95_total_ms: 0,
    avg_ttft_ms: 0,
    output_tps: 0,
    tps_samples: 0,
    output_tokens_per_second: 0,
    generation_tokens_per_second: 0,
    tokens_in: 0,
    tokens_out: 0,
    tokens_total: 0,
    estimated_cost: 0,
  },
]

function mockData(items = ITEMS) {
  mockedGet.mockImplementation(async (url: string) => {
    if (url === URL) {
      return {
        data: { from: 1, to: 2, items },
      } as never
    }
    throw new Error(`Unexpected GET ${url}`)
  })
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(() => {
  cleanup()
})

describe('渠道速度面板', () => {
  it('shows per-channel speed and idle/paused states on the fixed realtime window', async () => {
    mockData()
    renderConsolePage(<ChannelSpeedPanel />)

    expect(await screen.findByText('channel-fast')).toBeInTheDocument()
    expect(await screen.findByText('55.6')).toBeInTheDocument()
    expect(screen.getByText('channel-slow')).toBeInTheDocument()
    expect(screen.getByText('8.89')).toBeInTheDocument()
    expect(screen.getByText('channel-idle')).toBeInTheDocument()
    expect(screen.getByText('Idle')).toBeInTheDocument()
    expect(screen.getByText('Paused')).toBeInTheDocument()
    // 两个渠道有请求；摘要显示活跃数 / 总数，整体输出速度 58000/900=64.4。
    expect(screen.getByText('2 / 4')).toBeInTheDocument()
    expect(screen.getByText('64.4')).toBeInTheDocument()
    expect(screen.getAllByText('Requests/s').length).toBeGreaterThan(0)
    // 实时流量固定 15m：60000/900=66.7，11000/900=12.2，总计 78.9。
    expect(screen.getAllByText('Live traffic').length).toBeGreaterThan(0)
    expect(screen.getByText('66.7 tok/s')).toBeInTheDocument()
    expect(screen.getByText('12.2 tok/s')).toBeInTheDocument()
    expect(screen.getByText('78.9')).toBeInTheDocument()
    expect(
      screen.getByText(
        'Output speed = all completion tokens in the window; live traffic = input + output tokens over the last 15 minutes.'
      )
    ).toBeInTheDocument()
    // 时间档位已移除：渠道速度只显示实时值。
    expect(screen.queryByRole('tab')).not.toBeInTheDocument()
    expect(screen.getByText('Live · 30s · 5s')).toBeInTheDocument()
  })

  it('keeps all configured channels visible when the window has no traffic', async () => {
    mockData(
      ITEMS.map((item) => ({
        ...item,
        requests: 0,
        output_tps: 0,
        output_tokens_per_second: 0,
        tokens_in: 0,
        tokens_out: 0,
        tokens_total: 0,
      }))
    )
    renderConsolePage(<ChannelSpeedPanel />)

    await waitFor(() => {
      expect(
        screen.getAllByText('No channel traffic in this window.').length
      ).toBeGreaterThan(0)
    })
    // 卡片仍然出现，速度显示 —，不是 0。
    expect(screen.getByText('channel-fast')).toBeInTheDocument()
    expect(screen.getByText('channel-idle')).toBeInTheDocument()
    expect(screen.getByText('0 / 4')).toBeInTheDocument()
  })

  it('requests the 30s realtime window plus the fixed 15m live-traffic window', async () => {
    mockData()
    renderConsolePage(<ChannelSpeedPanel />)

    expect(await screen.findByText('channel-fast')).toBeInTheDocument()
    await waitFor(() => {
      const spans = mockedGet.mock.calls
        .map((call) => {
          const params = (
            call[1] as { params?: { from?: number; to?: number } } | undefined
          )?.params
          if (params?.from == null || params.to == null) return 0
          return params.to - params.from
        })
        .filter((span) => span > 0)
      expect(spans).toEqual(expect.arrayContaining([30, 15 * 60]))
    })
    expect(screen.getByText('66.7 tok/s')).toBeInTheDocument()
  })

  it('shows a readable error and retry action', async () => {
    mockedGet.mockRejectedValue(new Error('boom'))
    renderConsolePage(<ChannelSpeedPanel />)

    expect(
      await screen.findByText('Channel speed is unavailable')
    ).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
    })
  })
})
