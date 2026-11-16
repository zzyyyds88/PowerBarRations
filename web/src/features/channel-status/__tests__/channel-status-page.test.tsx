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
import userEvent from '@testing-library/user-event'
/*
渠道状态页（Connections → Channel status）的用户可见行为：

- 一行一个渠道，先看渠道级状态（服务中/降级/不可用/已暂停/未接入路由）；
- 展开渠道后显示「模型状态」，模型无样本显示 `-`，不拿 0 冒充失败；
- `24h` / `7d` 切换会以新 window 重新取数；
- 空态与错误态都不白屏。

HTTP 层是可控边界：只 stub `@/lib/api` 的 axios 实例，页面与 API 客户端全部走真实实现。
*/
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { renderPage as renderConsolePage } from '@/test-utils/render-page'

import { ChannelStatus } from '..'

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

const STATUS_LIST_URL = '/api/channels/status'

const STATUS_ITEMS = [
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
    last_error_kind: 'upstream_reject',
  },
  {
    channel: 'channel-b',
    enabled: false,
    type: 'ollama',
    lane_count: 0,
    member_count: 0,
    available_count: 0,
    degraded: true,
    requests: 0,
    failures: 0,
    success_rate: 0,
    estimated_cost: 0,
    last_error_kind: '',
  },
]

const MODELS = [
  {
    model: 'deepseek-v4.1-flash',
    requests: 120,
    successes: 118,
    failures: 2,
    success_rate: 0.9833,
    p50_total_ms: 620,
    p95_total_ms: 1200,
    avg_ttft_ms: 220,
    output_tps: 41.2,
    tps_samples: 96,
    tokens_out: 56000,
  },
  {
    model: 'glm-5.3-flash',
    requests: 0,
    successes: 0,
    failures: 0,
    success_rate: 0,
    p50_total_ms: 0,
    p95_total_ms: 0,
    avg_ttft_ms: 0,
    output_tps: 0,
    tps_samples: 0,
    tokens_out: 0,
  },
]

const DETAIL = {
  channel: {
    name: 'channel-a',
    enabled: true,
    type: 'openai',
    base_url: 'https://vendor.example/v1',
    key_set: true,
    key_prefix: 'sk-a',
    models: ['deepseek-v4.1-flash', 'glm-5.3-flash'],
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
    p50_total_ms: 620,
    p95_total_ms: 1200,
    avg_ttft_ms: 220,
    estimated_cost: 0.12,
  },
  models: MODELS,
  daily: [],
  recent_failures: [],
  probe_history: [],
  upstream_updates: {
    last_check_ts: null,
    pending_add: [],
    pending_remove: [],
  },
}

function stubPage(
  options: {
    items?: unknown[]
    details?: Record<string, unknown>
    statusListError?: boolean
  } = {}
) {
  mockedGet.mockImplementation(async (url: string) => {
    if (url === STATUS_LIST_URL) {
      if (options.statusListError) throw new Error('boom')
      return {
        data: { window: '24h', items: options.items ?? STATUS_ITEMS },
      } as never
    }
    if (url.startsWith('/api/channels/') && url.endsWith('/status')) {
      const name = url.slice('/api/channels/'.length, -'/status'.length)
      if (name === 'channel-b') {
        // 停用渠道没有流量：不给模型卡片，避免污染模型名断言。
        return {
          data: {
            ...DETAIL,
            channel: { ...DETAIL.channel, name, enabled: false },
            models: [],
          },
        } as never
      }
      return { data: options.details?.[name] ?? DETAIL } as never
    }
    throw new Error(`Unexpected GET ${url}`)
  })
}

function renderPage() {
  return renderConsolePage(<ChannelStatus />, { path: '/connections/health' })
    .client
}

/** 已发起的全量状态请求（含 window 参数）。 */
function listWindows(): Array<string | undefined> {
  return mockedGet.mock.calls
    .filter(([url]) => url === STATUS_LIST_URL)
    .map(([, config]) => (config?.params as { window?: string })?.window)
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(() => {
  cleanup()
})

describe('渠道状态页', () => {
  it('renders channel health first and expands model details', async () => {
    stubPage()
    renderPage()

    // 渠道是主行：channel-a 服务中，channel-b 已暂停。
    expect(await screen.findByText('channel-a')).toBeInTheDocument()
    expect(screen.getByText('channel-b')).toBeInTheDocument()
    expect(screen.getByText('Serving')).toBeInTheDocument()
    expect(screen.getByText('Paused')).toBeInTheDocument()

    // 成功率取自渠道汇总（120 请求 / 2 失败），不是 0%。
    expect(screen.getAllByText('98.33%').length).toBeGreaterThanOrEqual(2)

    // 默认不展开模型明细；展开后才显示模型、p50 与 tok/s。
    expect(screen.queryByText('deepseek-v4.1-flash')).not.toBeInTheDocument()

    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: /channel-a/i }))

    expect(await screen.findByText('deepseek-v4.1-flash')).toBeInTheDocument()
    expect(screen.getAllByText(/620ms/).length).toBeGreaterThan(0)
    expect(screen.getByText('41.2 tok/s')).toBeInTheDocument()

    // 无样本模型不显示 0%。
    expect(screen.getByText('glm-5.3-flash')).toBeInTheDocument()
    expect(screen.getAllByText('-').length).toBeGreaterThanOrEqual(3)
  })

  it('refetches the channels with the newly selected window', async () => {
    stubPage()
    renderPage()

    await screen.findByText('channel-a')
    expect(listWindows()).toEqual(['24h'])

    mockedGet.mockImplementation(async (url: string) => {
      if (url === STATUS_LIST_URL) {
        return { data: { window: '7d', items: STATUS_ITEMS } } as never
      }
      if (url.startsWith('/api/channels/') && url.endsWith('/status')) {
        return { data: { ...DETAIL, window: '7d' } } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })

    await userEvent.setup().click(screen.getByRole('tab', { name: '7d' }))

    await waitFor(() => {
      expect(listWindows()).toEqual(['24h', '7d'])
    })
  })

  it('shows the empty state when no channels are reporting', async () => {
    stubPage({ items: [], details: {} })
    renderPage()

    expect(await screen.findByText('No channel status yet')).toBeInTheDocument()
  })

  it('shows a retryable error state when the status endpoint fails', async () => {
    stubPage({ statusListError: true })
    renderPage()

    expect(
      await screen.findByText('Channel status is unavailable')
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
  })
})
