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
渠道状态详情抽屉的用户可见行为（ui-spec §6.4 五块内容 + 验收口径）：

- 打开后能看到当前状态、窗口概览、最近失败、探活历史、上游模型变动五块；
- `24h` / `7d` 切换会以新 window 重新取数；
- 点「立即探活」调用探活接口（`POST /api/channels/{name}/test`）并重取详情，
  历史里出现新记录；
- `upstream_reject` 只标注"上游拒绝过"，不当作渠道故障。

HTTP 层是可控边界：只 stub `@/lib/api` 的 axios 实例，状态 API 客户端与抽屉
组件全部走真实实现（含 §6.12 的字段口径）。
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import dayjs from '@/lib/dayjs'

import { formatStatusTime } from '../../../lib/channel-status-display'
import type { ChannelStatusDetail } from '../../../status-api'
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
const mockedPost = vi.mocked(api.post)

const CHANNEL_STATUS_URL = '/api/channels/channel-a/status'
const CHANNEL_PROBE_URL = '/api/channels/channel-a/test'

function makeChannel(overrides: Partial<Channel> = {}): Channel {
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
    ...overrides,
  })
}

/** api-spec §6.12 的响应示例（字段逐个对齐契约正本）。 */
function makeDetail(
  overrides: Partial<ChannelStatusDetail> = {}
): ChannelStatusDetail {
  return {
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
    reference: {
      lanes: [
        {
          lane: 'lane-alpha',
          model: 'model-1',
          upstream_model: 'model-1',
          priority: 10,
          enabled: true,
        },
      ],
    },
    runtime: {
      member_count: 1,
      available_count: 1,
      members: [
        {
          lane: 'lane-alpha',
          member: 'channel-a:model-1',
          enabled: true,
          available: true,
          circuit: 'closed',
          cooldown_until: null,
          circuit_open_until: null,
          consecutive_failures: 0,
          failure_score: 0,
          rolling_success_rate: 1,
          last_error_kind: '',
        },
      ],
    },
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
      {
        // 无样本模型：成功率与输出速度都必须显示占位符，不能显示 0。
        model: 'model-2',
        requests: 0,
        successes: 0,
        failures: 0,
        success_rate: 0,
        avg_total_ms: 0,
        avg_ttft_ms: 0,
        output_tps: 0,
        tokens_out: 0,
      },
    ],
    daily: [
      {
        date: '2026-10-04',
        requests: 30,
        successes: 30,
        failures: 0,
        estimated_cost: 0.03,
      },
    ],
    recent_failures: [
      {
        id: 123,
        ts: '2026-10-09T12:00:00Z',
        lane: 'lane-alpha',
        model: 'model-1',
        upstream_model: 'model-1',
        http_status: 500,
        error_kind: 'soft_transient',
        error_summary: 'upstream 500',
      },
    ],
    probe_history: [
      {
        ts: '2026-10-09T12:00:00Z',
        upstream_model: 'model-1',
        status: 'success',
        duration_ms: 320,
        http_status: 200,
        error_kind: '',
        msg: '',
      },
    ],
    upstream_updates: {
      last_check_ts: '2026-10-09T11:00:00Z',
      pending_add: ['model-2'],
      pending_remove: [],
    },
    ...overrides,
  }
}

function stubStatus(detail: ChannelStatusDetail) {
  mockedGet.mockImplementation(async (url: string) => {
    if (url === CHANNEL_STATUS_URL) return { data: detail } as never
    throw new Error(`Unexpected GET ${url}`)
  })
}

function renderDrawer() {
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
  return client
}

/** 已发起的状态详情请求（含 window 参数）。 */
function statusRequests(): Array<string | undefined> {
  return mockedGet.mock.calls
    .filter(([url]) => url === CHANNEL_STATUS_URL)
    .map(([, config]) => (config?.params as { window?: string })?.window)
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(() => {
  cleanup()
})

describe('ChannelStatusDrawer 六块内容', () => {
  it('renders current status, model status, window overview, failures and probes', async () => {
    stubStatus(makeDetail())
    renderDrawer()

    // 1. 当前状态：启用徽章、协议徽章、被引用车道、成员运行态。
    expect(await screen.findByText('Current status')).toBeInTheDocument()
    // 「Enabled」同时出现在状态徽章与被引用车道的成员状态列，两处都成立。
    expect(screen.getAllByText('Enabled').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('OpenAI compatible')).toBeInTheDocument()
    expect(screen.getByText('Referenced lanes')).toBeInTheDocument()
    // 引用表、成员运行态表与最近失败表各出现一次 lane-alpha。
    expect(screen.getAllByText('lane-alpha').length).toBe(3)
    expect(screen.getByText('Member runtime')).toBeInTheDocument()
    expect(screen.getByText('channel-a:model-1')).toBeInTheDocument()
    // 「Available」是成员运行态表的列头，也是该成员当前的可用徽章。
    expect(screen.getAllByText('Available').length).toBe(2)
    expect(screen.getByText('Closed')).toBeInTheDocument()
    // 0~1 的成功率按百分比展示（成员滚动成功率 100%）。
    expect(screen.getByText('100.00%')).toBeInTheDocument()

    // 2. 模型状态：一行一个上游模型，成功率 / 延迟 / 输出速度 / 输出 token。
    expect(screen.getByText('Model status')).toBeInTheDocument()
    expect(screen.getByText('Output speed (tps)')).toBeInTheDocument()
    expect(screen.getByText('Output tokens')).toBeInTheDocument()
    // 41.2 tps 与输出 token 只来自模型行（token 走数量级格式化：英文语言包 → 56.0K）。
    expect(screen.getByText('41.2')).toBeInTheDocument()
    expect(screen.getByText('56.0K')).toBeInTheDocument()
    // 成功率与输出速度都出现在模型行（无样本的 model-2 显示占位符，不显示 0）。
    expect(screen.getAllByText('98.33%').length).toBeGreaterThanOrEqual(2)
    // 无样本模型（model-2）：成功率/平均延迟/平均首字/输出速度四个占位符，
    // 不显示 0 误导。
    const noSampleRow = screen.getByText('model-2').closest('tr')
    if (!noSampleRow) throw new Error('model-2 row not found')
    expect(within(noSampleRow).getAllByText('-')).toHaveLength(4)

    // 3. 窗口概览：指标与逐日柱状图（只用 daily 的日粒度）。
    expect(screen.getByText('Window overview')).toBeInTheDocument()
    // 「Success rate」是窗口概览的指标，也是模型状态表的列头。
    expect(screen.getAllByText('Success rate').length).toBeGreaterThanOrEqual(2)
    // 汇总成功率与模型行的成功率同值，两处都在。
    expect(screen.getAllByText('98.33%').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('¥0.12')).toBeInTheDocument()
    expect(screen.getByText('Daily requests and failures')).toBeInTheDocument()
    expect(screen.getByText('10-04')).toBeInTheDocument()

    // 4. 最近失败：时间/车道/模型/状态码/error_kind/摘要。
    expect(screen.getByText('Recent failures')).toBeInTheDocument()
    expect(screen.getByText('soft_transient')).toBeInTheDocument()
    expect(screen.getByText('upstream 500')).toBeInTheDocument()
    expect(screen.getByText('500')).toBeInTheDocument()

    // 5. 探活历史：本次探活记录与耗时。
    expect(screen.getByText('Probe history')).toBeInTheDocument()
    expect(screen.getByText('320ms')).toBeInTheDocument()

    // 6. 上游模型变动：上次检测时间与待处理清单。
    expect(screen.getByText('Upstream model changes')).toBeInTheDocument()
    expect(
      screen.getByText(formatStatusTime('2026-10-09T11:00:00Z'))
    ).toBeInTheDocument()
    expect(screen.getByText('+model-2')).toBeInTheDocument()
  })

  it('shows the cooldown deadline only while the cooldown is still active', async () => {
    const active = dayjs().add(5, 'minute').toISOString()
    const expired = dayjs().subtract(5, 'minute').toISOString()
    stubStatus(
      makeDetail({
        runtime: {
          member_count: 2,
          available_count: 0,
          members: [
            {
              lane: 'lane-alpha',
              member: 'channel-a:model-1',
              enabled: true,
              available: false,
              circuit: 'closed',
              cooldown_until: active,
              circuit_open_until: null,
              consecutive_failures: 2,
              failure_score: 3,
              rolling_success_rate: 0.5,
              last_error_kind: 'soft_rate_limit',
            },
            {
              lane: 'lane-alpha',
              member: 'channel-a:model-2',
              enabled: true,
              available: true,
              circuit: 'closed',
              cooldown_until: expired,
              circuit_open_until: null,
              consecutive_failures: 0,
              failure_score: 0,
              rolling_success_rate: 1,
              last_error_kind: '',
            },
          ],
        },
      })
    )
    renderDrawer()

    expect(await screen.findByText('channel-a:model-1')).toBeInTheDocument()
    expect(screen.getByText(formatStatusTime(active))).toBeInTheDocument()
    // 已过期的冷却不再展示时刻，也不谎报"故障"。
    expect(
      screen.queryByText(formatStatusTime(expired))
    ).not.toBeInTheDocument()
    expect(screen.getByText('50.00%')).toBeInTheDocument()
  })

  it('flags upstream_reject without claiming a channel fault', async () => {
    const detail = makeDetail()
    stubStatus({
      ...detail,
      runtime: {
        ...detail.runtime,
        members: [
          {
            ...detail.runtime.members[0],
            last_error_kind: 'upstream_reject',
          },
        ],
      },
      recent_failures: [
        { ...detail.recent_failures[0], error_kind: 'upstream_reject' },
      ],
    })
    renderDrawer()

    expect(
      await screen.findAllByText(
        'Upstream rejected the request (not a channel fault).'
      )
    ).toHaveLength(2)
    expect(screen.getAllByText('upstream_reject').length).toBeGreaterThan(0)
  })

  it('renders a readable error state with retry when the endpoint fails', async () => {
    mockedGet.mockRejectedValue(new Error('boom'))
    renderDrawer()

    expect(
      await screen.findByText('Channel status is unavailable')
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
  })
})

describe('ChannelStatusDrawer 窗口切换', () => {
  it('refetches the detail with the newly selected window', async () => {
    stubStatus(makeDetail())
    renderDrawer()

    await screen.findByText('Current status')
    expect(statusRequests()).toEqual(['24h'])

    mockedGet.mockImplementation(async (url: string) => {
      if (url === CHANNEL_STATUS_URL) {
        return {
          data: makeDetail({
            window: '7d',
            summary: {
              requests: 700,
              successes: 700,
              failures: 0,
              success_rate: 1,
              avg_total_ms: 500,
              avg_ttft_ms: 100,
              estimated_cost: 1,
            },
          }),
        } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })

    await userEvent.setup().click(screen.getByRole('tab', { name: '7d' }))

    await waitFor(() => {
      expect(statusRequests()).toEqual(['24h', '7d'])
    })
    // 7d 的数字是第二次取数的结果（平均耗时 500ms、折算成本 ¥1.00）。
    expect(await screen.findByText('500ms')).toBeInTheDocument()
    expect(screen.getByText('¥1.00')).toBeInTheDocument()
  })
})

describe('ChannelStatusDrawer 上游变动检测', () => {
  it('runs the existing detection flow and reloads the pending list', async () => {
    let detected = false
    mockedGet.mockImplementation(async (url: string) => {
      if (url !== CHANNEL_STATUS_URL) {
        throw new Error(`Unexpected GET ${url}`)
      }
      return {
        data: makeDetail({
          upstream_updates: {
            last_check_ts: detected ? '2026-10-09T14:00:00Z' : null,
            pending_add: detected ? ['model-3'] : [],
            pending_remove: [],
          },
        }),
      } as never
    })
    mockedPost.mockImplementation(async (url: string) => {
      if (url !== '/api/channel/upstream_updates/detect') {
        throw new Error(`Unexpected POST ${url}`)
      }
      detected = true
      return { data: { add_models: ['model-3'], remove_models: [] } } as never
    })
    renderDrawer()

    // 从未检测过 → 空态文案；检测后出现待新增清单。
    expect(await screen.findByText('Never checked')).toBeInTheDocument()

    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'Detect now' }))

    await waitFor(() => {
      expect(mockedPost.mock.calls[0]?.[0]).toBe(
        '/api/channel/upstream_updates/detect'
      )
    })
    expect(mockedPost.mock.calls[0]?.[1]).toEqual({ id: 7 })
    expect(await screen.findByText('+model-3')).toBeInTheDocument()
    expect(
      screen.getByText(formatStatusTime('2026-10-09T14:00:00Z'))
    ).toBeInTheDocument()
  })
})

describe('ChannelStatusDrawer 立即探活', () => {
  it('probes the channel and reloads the detail so the history gains a record', async () => {
    const probeRecord = {
      ts: '2026-10-09T13:00:00Z',
      upstream_model: 'model-1',
      status: 'success',
      duration_ms: 250,
      http_status: 200,
      error_kind: '',
      msg: '',
    }
    let probed = false
    mockedGet.mockImplementation(async (url: string) => {
      if (url !== CHANNEL_STATUS_URL) {
        throw new Error(`Unexpected GET ${url}`)
      }
      return {
        data: makeDetail({
          probe_history: probed ? [probeRecord] : [],
        }),
      } as never
    })
    mockedPost.mockImplementation(async (url: string) => {
      if (url !== CHANNEL_PROBE_URL) {
        throw new Error(`Unexpected POST ${url}`)
      }
      probed = true
      return {
        data: {
          channel: 'channel-a',
          upstream_model: 'model-1',
          status: 'success',
          duration_ms: 250,
          ok: true,
          status_code: 200,
          latency_ms: 250,
        },
      } as never
    })
    renderDrawer()

    expect(await screen.findByText('No probe history yet')).toBeInTheDocument()

    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'Probe now' }))

    await waitFor(() => {
      expect(mockedPost.mock.calls[0]?.[0]).toBe(CHANNEL_PROBE_URL)
    })
    // 探活完成后重取详情：历史出现新记录、运行态随之刷新。
    expect(await screen.findByText('250ms')).toBeInTheDocument()
    await waitFor(() => {
      expect(statusRequests().length).toBeGreaterThanOrEqual(2)
    })
    expect(screen.queryByText('No probe history yet')).not.toBeInTheDocument()
  })

  it('surfaces a failed probe without dropping the probe record', async () => {
    let probed = false
    mockedGet.mockImplementation(async (url: string) => {
      if (url !== CHANNEL_STATUS_URL) {
        throw new Error(`Unexpected GET ${url}`)
      }
      return {
        data: makeDetail({
          probe_history: probed
            ? [
                {
                  ts: '2026-10-09T13:05:00Z',
                  upstream_model: 'model-1',
                  status: 'failed',
                  duration_ms: 8000,
                  http_status: 0,
                  error_kind: 'soft_transient',
                  msg: 'non-stream response timeout',
                },
              ]
            : [],
        }),
      } as never
    })
    mockedPost.mockImplementation(async () => {
      probed = true
      return {
        data: {
          channel: 'channel-a',
          upstream_model: 'model-1',
          status: 'failed',
          duration_ms: 8000,
          error_kind: 'timeout',
          msg: 'non-stream response timeout',
          ok: false,
          status_code: 0,
          latency_ms: 8000,
        },
      } as never
    })
    renderDrawer()

    expect(await screen.findByText('No probe history yet')).toBeInTheDocument()
    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'Probe now' }))

    expect(
      await screen.findByText('non-stream response timeout')
    ).toBeInTheDocument()
    expect(screen.getByText('Failed')).toBeInTheDocument()
  })
})
