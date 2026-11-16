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
import { cleanup, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
/*
实时路由卡（已迁入概览）：车道选择、当前成员/限流倒计时、最近请求 attempts 链。
HTTP 与 SSE 是可控边界；卡片逻辑、查询与渲染走真实实现。
*/
import dayjs from 'dayjs'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { renderPage as renderConsolePage } from '@/test-utils/render-page'

import { LiveRoutingCard } from '../components/live-routing-card'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
  getFreshAuthHeaders: vi.fn(async () => ({})),
}))

vi.mock('@/lib/route-events', async (importOriginal) => {
  const mod = (await importOriginal()) as typeof import('@/lib/route-events')
  return {
    ...mod,
    openRouteEventStream: vi.fn(() => ({ close: vi.fn() })),
  }
})

const mockedGet = vi.mocked(api.get)

const LANES = [
  {
    name: 'lane-a',
    enabled: true,
    mode: 'failover',
    active_member: '',
    orphan_member_count: 0,
    members: [
      {
        channel: 'alpha',
        model: 'model',
        upstream_model: 'model',
        priority: 10,
        enabled: true,
        channel_enabled: true,
      },
      {
        channel: 'beta',
        model: 'model',
        upstream_model: 'model',
        priority: 5,
        enabled: true,
        channel_enabled: true,
      },
    ],
  },
  {
    name: 'lane-b',
    enabled: true,
    mode: 'failover',
    active_member: '',
    orphan_member_count: 0,
    members: [
      {
        channel: 'gamma',
        model: 'model',
        upstream_model: 'model',
        priority: 10,
        enabled: true,
        channel_enabled: true,
      },
    ],
  },
]

const HEALTH = {
  'lane-a': {
    lane: 'lane-a',
    source: 'explicit',
    mode: 'failover',
    current_member: 'alpha/model',
    probe_member: '',
    affinity: null,
    members: [
      {
        member: 'alpha/model',
        channel: 'alpha',
        upstream_model: 'model',
        circuit: 'closed',
        consecutive_failures: 0,
        failure_score: 0,
        rolling_success_rate: 1,
        cooldown_until: null,
        circuit_open_until: null,
        current: true,
        probing: false,
        available: true,
        enabled: true,
      },
      {
        member: 'beta/model',
        channel: 'beta',
        upstream_model: 'model',
        circuit: 'closed',
        consecutive_failures: 0,
        failure_score: 0,
        rolling_success_rate: 0,
        cooldown_until: dayjs().add(30, 'second').toISOString(),
        circuit_open_until: null,
        last_error_kind: 'soft_rate_limit',
        current: false,
        probing: false,
        available: false,
        enabled: true,
      },
    ],
  },
  'lane-b': {
    lane: 'lane-b',
    source: 'explicit',
    mode: 'failover',
    current_member: '',
    probe_member: '',
    affinity: null,
    members: [
      {
        member: 'gamma/model',
        channel: 'gamma',
        upstream_model: 'model',
        circuit: 'closed',
        consecutive_failures: 0,
        failure_score: 0,
        rolling_success_rate: 1,
        cooldown_until: null,
        circuit_open_until: null,
        current: false,
        probing: false,
        available: true,
        enabled: true,
      },
    ],
  },
}

const STATS = [
  {
    bucket_ts: 1790000000,
    group: 'lane-a',
    requests: 8,
    successes: 7,
    prompt_tokens: 0,
    completion_tokens: 0,
    estimated_cost: 0,
  },
  {
    bucket_ts: 1790000000,
    group: 'lane-b',
    requests: 2,
    successes: 2,
    prompt_tokens: 0,
    completion_tokens: 0,
    estimated_cost: 0,
  },
]

const LOG = {
  id: 1,
  ts: dayjs().toISOString(),
  lane: 'lane-a',
  request_model: 'lane-a',
  route_source: 'explicit',
  channel: 'alpha',
  channel_id: 1,
  upstream_model: 'model',
  key_name: 'mock-key',
  token_id: 1,
  user_id: 1,
  username: '',
  type: 1,
  ip: '',
  inbound_format: 'openai',
  success: true,
  http_status: 200,
  error_kind: '',
  error_summary: '',
  prompt_tokens: 1,
  completion_tokens: 2,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
  reasoning_tokens: 0,
  ttft_ms: 100,
  total_ms: 1200,
  is_stream: true,
  attempts: [
    {
      attempt_num: 1,
      member: 'alpha/model',
      status: 'success',
      duration_ms: 1200,
    },
  ],
  total_attempts: 1,
  estimated_cost: 0,
}

const RANGE = { from: 1790000000, to: 1790086400, key: '24h' }

function stubCard(options: { lanes?: unknown[] } = {}) {
  mockedGet.mockImplementation(async (url: string) => {
    if (url === '/api/v1/lane-summaries') {
      return { data: { items: options.lanes ?? LANES } } as never
    }
    if (url === '/api/stats') {
      return {
        data: { granularity: 'hour', group_by: 'lane', items: STATS },
      } as never
    }
    if (url.startsWith('/api/v1/lanes/') && url.endsWith('/health')) {
      const name = decodeURIComponent(
        url.slice('/api/v1/lanes/'.length, -'/health'.length)
      )
      return { data: HEALTH[name as keyof typeof HEALTH] } as never
    }
    if (url.startsWith('/api/v1/lanes/')) {
      return {
        data: { mode: 'failover', active_member: '', config: {} },
      } as never
    }
    if (url === '/api/v1/logs') {
      return { data: { items: [LOG], next_cursor: null } } as never
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

describe('实时路由卡', () => {
  it('renders current/cooldown states, recent attempts and lane switching', async () => {
    stubCard()
    const onLaneChange = vi.fn()
    renderConsolePage(
      <LiveRoutingCard
        range={RANGE}
        lane='lane-a'
        onLaneChange={onLaneChange}
      />,
      { path: '/overview' }
    )

    expect(await screen.findByText('Live routing')).toBeInTheDocument()
    expect(await screen.findByText('Current member')).toBeInTheDocument()
    expect(screen.getByText(/Rate limited/)).toBeInTheDocument()
    expect(screen.getByText(/First available/)).toBeInTheDocument()
    expect(screen.getByText('Recent request')).toBeInTheDocument()
    expect(screen.getByText('mock-key')).toBeInTheDocument()

    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: /lane-b/ }))
    expect(onLaneChange).toHaveBeenCalledWith('lane-b')
  })

  it('shows the empty state when no lanes exist', async () => {
    stubCard({ lanes: [] })
    renderConsolePage(<LiveRoutingCard range={RANGE} />, { path: '/overview' })

    expect(await screen.findByText('No lanes yet')).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Open routing' })
    ).toBeInTheDocument()
  })

  it('hides its own heading when the page already provides one', async () => {
    stubCard()
    renderConsolePage(<LiveRoutingCard range={RANGE} hideTitle />, {
      path: '/live-routing',
    })

    expect(await screen.findByText('Current member')).toBeInTheDocument()
    expect(
      screen.queryByRole('heading', { name: 'Live routing' })
    ).not.toBeInTheDocument()
    expect(screen.getByText(/^(Live|Polling)$/)).toBeInTheDocument()
  })
})
