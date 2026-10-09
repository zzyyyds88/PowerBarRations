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
渠道状态详情抽屉的空态契约（ui-spec §6.4 验收）：从未探活过的渠道、没有任何车道
引用的渠道、没有任何失败/日数据的窗口都必须给出可读空态文案，而不是空白或崩溃。
口径来源 api-spec §6.12：未被任何车道引用时 `runtime.members` 为空数组；从未探测过
上游变动时 `last_check_ts` 为 null 且两个清单为空。
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { channelSchema, type Channel } from '../../../types'
import { ChannelsProvider } from '../../channels-provider'
import { ChannelStatusDrawer } from '../channel-status-drawer'

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

function makeChannel(): Channel {
  return channelSchema.parse({
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
  })
}

/** 全部数据为空的渠道：无引用车道、无成员、无请求、无失败、无探活、无上游快照。 */
const EMPTY_DETAIL = {
  channel: {
    name: 'channel-a',
    enabled: false,
    type: 'openai',
    base_url: 'https://vendor.example/v1',
    key_set: true,
    key_prefix: 'sk-a',
    models: [],
    model_mapping: {},
  },
  reference: { lanes: [] },
  runtime: { member_count: 0, available_count: 0, members: [] },
  window: '24h',
  summary: {
    requests: 0,
    successes: 0,
    failures: 0,
    success_rate: 0,
    avg_total_ms: 0,
    avg_ttft_ms: 0,
    estimated_cost: 0,
  },
  models: [],
  daily: [],
  recent_failures: [],
  probe_history: [],
  upstream_updates: {
    last_check_ts: null,
    pending_add: [],
    pending_remove: [],
  },
}

function renderEmptyDrawer() {
  mockedGet.mockImplementation(async (url: string) => {
    if (url === '/api/channels/channel-a/status') {
      return { data: EMPTY_DETAIL } as never
    }
    throw new Error(`Unexpected GET ${url}`)
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <ChannelStatusDrawer
          open
          onOpenChange={() => undefined}
          channel={makeChannel()}
        />
      </ChannelsProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(() => {
  cleanup()
})

it('replaces every empty block with readable copy instead of blank space', async () => {
  renderEmptyDrawer()

  // 未被任何车道引用（配置态）与没有被引用的成员运行态是两件事，各自明示。
  expect(
    await screen.findByText('Not referenced by any lane')
  ).toBeInTheDocument()
  expect(screen.getByText('No lane members')).toBeInTheDocument()
  expect(screen.getByText('Available members: 0/0')).toBeInTheDocument()

  // 窗口内没有请求：模型状态表与逐日图各自空态文案（不编造数据）。
  expect(screen.getByText('Model status')).toBeInTheDocument()
  expect(screen.getByText('No model data in this window')).toBeInTheDocument()
  expect(screen.getByText('No requests in this window')).toBeInTheDocument()
  expect(screen.getByText('Successes')).toBeInTheDocument()

  // 历史与上游变动：各自空态文案，且从未检测过时明示。
  expect(screen.getByText('No recent failures')).toBeInTheDocument()
  expect(screen.getByText('No probe history yet')).toBeInTheDocument()
  expect(screen.getByText('Never checked')).toBeInTheDocument()
  expect(screen.getByText('No upstream model changes')).toBeInTheDocument()

  // 停用渠道的徽章如实展示，且五块标题都在（没有任何一块因空数据消失）。
  expect(screen.getByText('Disabled')).toBeInTheDocument()
  for (const title of [
    'Current status',
    'Model status',
    'Window overview',
    'Recent failures',
    'Probe history',
    'Upstream model changes',
  ]) {
    expect(screen.getByText(title)).toBeInTheDocument()
  }
})

it('keeps the probe action available when there is no probe history yet', async () => {
  renderEmptyDrawer()

  expect(await screen.findByText('No probe history yet')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Probe now' })).toBeEnabled()
})
