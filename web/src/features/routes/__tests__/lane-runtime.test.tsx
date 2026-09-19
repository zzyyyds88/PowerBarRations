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
车道运行态展示（ui-spec §4）的用户可见行为：
- 轮询兜底渲染成员级冷却/亲和/探测占用与车道 healthy/degraded；
- SSE 新鲜时 SSE 主导渲染、轮询只补 SSE 缺失的车道（对账不闪烁）；
- 组件卸载关闭 SSE 连接。
SSE 连接器与 axios 实例是可控边界：openRouteEventStream 桩捕获回调，
帧解析/对账等纯逻辑保持真实实现（其单测见 lib/__tests__/route-events.test.ts）。
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, screen } from '@testing-library/react'
import dayjs from '@/lib/dayjs'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import type {
  LaneHealthSnapshot,
  LaneMemberHealth,
  RouteEventStreamOptions,
} from '@/lib/route-events'

import { Routes } from '..'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
    post: vi.fn(),
  },
  getFreshAuthHeaders: vi.fn(async () => ({})),
}))

const sseCaptured: { opts: RouteEventStreamOptions | null; close: () => void } =
  { opts: null, close: vi.fn() }

vi.mock('@/lib/route-events', async (importOriginal) => {
  const mod =
    (await importOriginal()) as typeof import('@/lib/route-events')
  return {
    ...mod,
    openRouteEventStream: vi.fn((opts: RouteEventStreamOptions) => {
      sseCaptured.opts = opts
      return { close: sseCaptured.close }
    }),
  }
})

const mockedGet = vi.mocked(api.get)

function futureTime(minutes = 5): string {
  return dayjs().add(minutes, 'minute').toISOString()
}

function member(
  channel: string,
  extra?: Partial<LaneMemberHealth>
): LaneMemberHealth {
  return {
    member: `${channel}/model-1`,
    channel,
    upstream_model: 'model-1',
    circuit: 'closed',
    consecutive_failures: 0,
    rolling_success_rate: 1,
    cooldown_until: null,
    current: false,
    probing: false,
    available: true,
    ...extra,
  }
}

function laneSnapshot(
  lane: string,
  members: LaneMemberHealth[],
  extra?: Partial<LaneHealthSnapshot>
): LaneHealthSnapshot {
  return {
    lane,
    current_member: '',
    probe_member: '',
    affinity: null,
    members,
    ...extra,
  }
}

const EXPLICIT_MODEL_1 = {
  model: 'model-1',
  source: 'explicit',
  routable: true,
  member_count: 2,
  available_member_count: 2,
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Routes />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  sseCaptured.opts = null
  sseCaptured.close = vi.fn()
})

afterEach(() => {
  cleanup()
})

describe('车道运行态列（轮询兜底）', () => {
  test('SSE 未就绪且轮询未返回时显示等待文案', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        return { data: { items: [EXPLICIT_MODEL_1] } } as never
      }
      if (url === '/api/v1/lane-summaries') {
        return { data: { items: [] } } as never
      }
      if (url === '/api/v1/lanes/model-1/health') {
        throw new Error('health unavailable')
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    expect(await screen.findByText('Waiting for runtime data')).toBeInTheDocument()
  })

  test('轮询快照渲染成员冷却徽章与车道 degraded 汇总', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        return { data: { items: [EXPLICIT_MODEL_1] } } as never
      }
      if (url === '/api/v1/lane-summaries') {
        return { data: { items: [] } } as never
      }
      if (url === '/api/v1/lanes/model-1/health') {
        return {
          data: laneSnapshot('model-1', [
            member('channel-a', {
              circuit: 'closed',
              cooldown_until: futureTime(),
              available: false,
              consecutive_failures: 2,
            }),
            member('channel-b'),
          ]),
        } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    expect(await screen.findByText('channel-a · Cooldown')).toBeInTheDocument()
    expect(screen.getByText('Degraded')).toBeInTheDocument()
    // 无状态成员不出徽章，健康成员不占运行态噪音。
    expect(screen.queryByText('channel-b · Cooldown')).not.toBeInTheDocument()
  })

  test('冷却已过期的成员不再显示冷却徽章', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        return { data: { items: [EXPLICIT_MODEL_1] } } as never
      }
      if (url === '/api/v1/lane-summaries') {
        return { data: { items: [] } } as never
      }
      if (url === '/api/v1/lanes/model-1/health') {
        return {
          data: laneSnapshot('model-1', [
            member('channel-a', {
              cooldown_until: dayjs()
                .subtract(1, 'minute')
                .toISOString(),
            }),
            member('channel-b'),
          ]),
        } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    expect(await screen.findByText('Healthy')).toBeInTheDocument()
    expect(screen.queryByText('channel-a · Cooldown')).not.toBeInTheDocument()
  })

  test('当前成员/探测占用/亲和徽章按快照渲染', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        return { data: { items: [EXPLICIT_MODEL_1] } } as never
      }
      if (url === '/api/v1/lane-summaries') {
        return { data: { items: [] } } as never
      }
      if (url === '/api/v1/lanes/model-1/health') {
        return {
          data: laneSnapshot(
            'model-1',
            [
              member('channel-a', { current: true }),
              member('channel-b', { probing: true, circuit: 'half-open' }),
            ],
            {
              current_member: 'channel-a/model-1',
              probe_member: 'channel-b/model-1',
              affinity: {
                channel: 'channel-a',
                upstream_model: 'model-1',
                until: futureTime(),
              },
            }
          ),
        } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    expect(await screen.findByText('channel-a · Affinity')).toBeInTheDocument()
    expect(screen.getByText('channel-a · Current')).toBeInTheDocument()
    expect(screen.getByText('channel-b · Probing')).toBeInTheDocument()
    expect(screen.getByText('channel-b · Half-open')).toBeInTheDocument()
    expect(screen.getByText('Healthy')).toBeInTheDocument()
  })
})

describe('车道运行态列（SSE 主导与双源对账）', () => {
  test('SSE 新鲜时 SSE 主导渲染；轮询只为 SSE 缺失车道兜底', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        return {
          data: {
            items: [
              EXPLICIT_MODEL_1,
              {
                model: 'model-2',
                source: 'explicit',
                routable: true,
                member_count: 1,
                available_member_count: 1,
              },
            ],
          },
        } as never
      }
      if (url === '/api/v1/lane-summaries') {
        return { data: { items: [] } } as never
      }
      if (url === '/api/v1/lanes/model-1/health') {
        // 轮询说 model-1 一切正常——但 SSE（更新鲜）说它在熔断。
        return {
          data: laneSnapshot('model-1', [member('channel-a')]),
        } as never
      }
      if (url === '/api/v1/lanes/model-2/health') {
        return {
          data: laneSnapshot('model-2', [
            member('channel-c', {
              upstream_model: 'model-2',
              circuit: 'open',
              available: false,
            }),
          ]),
        } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()
    await screen.findByText('model-1')
    const opts = sseCaptured.opts
    expect(opts).not.toBeNull()

    act(() => {
      opts?.onStatus?.('open')
      opts?.onLanes(
        [
          laneSnapshot('model-1', [
            member('channel-a', {
              circuit: 'open',
              circuit_open_until: futureTime(2),
              available: false,
              last_error_kind: 'soft_transient',
            }),
          ]),
        ],
        dayjs().toISOString()
      )
    })

    // SSE 主导：model-1 显示熔断（而不是轮询的 Healthy）。
    expect(
      await screen.findByText('channel-a · Circuit open')
    ).toBeInTheDocument()
    expect(screen.queryByText('Healthy')).not.toBeInTheDocument()
    // 对账：SSE 没有 model-2，轮询结果只补这一条车道。
    expect(screen.getByText('channel-c · Circuit open')).toBeInTheDocument()
  })

  test('组件卸载关闭 SSE 连接', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        return { data: { items: [EXPLICIT_MODEL_1] } } as never
      }
      if (url === '/api/v1/lane-summaries') {
        return { data: { items: [] } } as never
      }
      if (url === '/api/v1/lanes/model-1/health') {
        throw new Error('offline')
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    const view = renderPage()
    await screen.findByText('Waiting for runtime data')
    expect(sseCaptured.opts).not.toBeNull()

    view.unmount()
    expect(sseCaptured.close).toHaveBeenCalledTimes(1)
  })
})
