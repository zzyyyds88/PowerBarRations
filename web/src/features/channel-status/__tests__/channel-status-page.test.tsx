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
渠道状态页 `/channel-status` 的用户可见行为（ui-spec §6.4A）：

- 一眼看全的状态表（渠道 / 协议 / 启用 / 车道 / 成员 / 降级 / 请求 / 失败 /
  成功率 / 折算成本 / 最近错误分类 / 上次探活 / 详情）；
- `24h` / `7d` 切换会以新 window 重新取数；
- 行内展开显示该渠道**每个上游模型**的成功率与输出速度（api-spec §6.12 的 models[]）；
- 行内「详情」打开状态抽屉；
- 空态与错误态都不白屏。

HTTP 层是可控边界：只 stub `@/lib/api` 的 axios 实例，页面与 API 客户端全部走真实实现。
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

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
const CONSOLE_CHANNELS_URL = '/api/channel/'
const CONSOLE_CHANNEL_DETAIL_URL = '/api/channels/channel-a/status'

/** 控制台渠道记录（协议列与抽屉记录用它；探测入口的 id 也来自这里）。 */
const CONSOLE_CHANNEL = {
  id: 7,
  name: 'channel-a',
  type: 1,
  key: '',
  status: 1,
  created_time: 1,
  test_time: 0,
  response_time: 0,
  balance_updated_time: 0,
  models: 'model-1',
  base_url: 'https://vendor.example/v1',
}

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
    last_probe: {
      ts: '2026-10-09T12:00:00Z',
      status: 'success',
      duration_ms: 320,
    },
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

const MODEL_STATS = [
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
]

const STATUS_DETAIL = {
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
  models: MODEL_STATS,
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
    detail?: unknown
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
    if (url === CONSOLE_CHANNELS_URL) {
      return {
        data: {
          items: [CONSOLE_CHANNEL],
          total: 1,
          page: 1,
          page_size: 100,
          type_counts: {},
        },
      } as never
    }
    if (url === CONSOLE_CHANNEL_DETAIL_URL) {
      return { data: options.detail ?? STATUS_DETAIL } as never
    }
    throw new Error(`Unexpected GET ${url}`)
  })
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ChannelStatus />
    </QueryClientProvider>
  )
  return client
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
  it('renders the channel status table with protocol, members and upstream_reject notice', async () => {
    stubPage()
    renderPage()

    expect(await screen.findByText('channel-a')).toBeInTheDocument()
    // 协议列：控制台记录优先（channel-a 是 OpenAI 兼容），适配器名兜底（ollama 原样）。
    expect(screen.getByText('OpenAI compatible')).toBeInTheDocument()
    expect(screen.getByText('ollama')).toBeInTheDocument()
    // 启用状态、成员（可用/总数）、降级。
    expect(screen.getByText('Enabled')).toBeInTheDocument()
    expect(screen.getByText('Disabled')).toBeInTheDocument()
    expect(screen.getByText('1/1')).toBeInTheDocument()
    // 「Degraded」既是列头也是 channel-b 的徽章。
    expect(screen.getAllByText('Degraded')).toHaveLength(2)
    expect(screen.getByText('Normal')).toBeInTheDocument()
    // 无请求的渠道成功率显示占位符，不显示 0%。
    expect(screen.getByText('98.33%')).toBeInTheDocument()
    // upstream_reject 提示：上游拒绝过不代表渠道故障。
    expect(
      screen.getByText('Upstream rejected the request (not a channel fault).')
    ).toBeInTheDocument()
    // 上次探活时间来自 last_probe。
    expect(screen.getByText('Last probe')).toBeInTheDocument()
  })

  it('refetches the table with the newly selected window', async () => {
    stubPage()
    renderPage()

    await screen.findByText('channel-a')
    expect(listWindows()).toEqual(['24h'])

    mockedGet.mockImplementation(async (url: string) => {
      if (url === STATUS_LIST_URL) {
        return { data: { window: '7d', items: STATUS_ITEMS } } as never
      }
      if (url === CONSOLE_CHANNELS_URL) {
        return { data: { items: [CONSOLE_CHANNEL] } } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })

    await userEvent.setup().click(screen.getByRole('tab', { name: '7d' }))

    await waitFor(() => {
      expect(listWindows()).toEqual(['24h', '7d'])
    })
  })

  it('expands a row inline to show per-model success rate and output speed', async () => {
    stubPage()
    renderPage()

    await screen.findByText('channel-a')
    expect(screen.queryByText('Model status')).not.toBeInTheDocument()

    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'channel-a' }))

    // 展开行按当前 window 取该渠道详情，列出每个上游模型的模型状态表。
    expect(await screen.findByText('Model status')).toBeInTheDocument()
    expect(screen.getByText('Output speed (tps)')).toBeInTheDocument()
    expect(screen.getByText('41.2')).toBeInTheDocument()
    expect(screen.getByText('56,000')).toBeInTheDocument()
    const detailCalls = mockedGet.mock.calls.filter(
      ([url]) => url === CONSOLE_CHANNEL_DETAIL_URL
    )
    expect(detailCalls).toHaveLength(1)
    expect((detailCalls[0]?.[1]?.params as { window?: string })?.window).toBe(
      '24h'
    )
  })

  it('opens the status drawer from the row detail button', async () => {
    stubPage()
    renderPage()

    await screen.findByText('channel-a')
    await userEvent
      .setup()
      .click(screen.getAllByRole('button', { name: 'Details' })[0])

    // 抽屉六块内容（这里是标题；内容断言由抽屉自己的测试覆盖）。
    expect(await screen.findByText('Current status')).toBeInTheDocument()
    expect(screen.getByText('Member runtime')).toBeInTheDocument()
    expect(screen.getByText('Window overview')).toBeInTheDocument()
    expect(screen.getByText('Upstream model changes')).toBeInTheDocument()
  })

  it('shows the empty state when no channel exists yet', async () => {
    stubPage({ items: [] })
    renderPage()

    expect(await screen.findByText('No channels yet')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Details' })).toBeNull()
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
