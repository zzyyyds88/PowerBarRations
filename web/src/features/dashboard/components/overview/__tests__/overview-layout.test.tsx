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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { startOfLocalDaySeconds } from '../../../lib/time-range'
import { PBR_CHANNEL_MODEL_SEPARATOR } from '../../../pbr-stats-api'
import { OverviewDashboard } from '../overview-dashboard'

type RecordedCall = { url: string; params: Record<string, unknown> }

let client: QueryClient
let calls: RecordedCall[]

const BUCKET_TS = 1_700_000_000

function statsBucket(group: string, requests: number, cost: number) {
  return {
    bucket_ts: BUCKET_TS,
    group,
    requests,
    successes: requests,
    prompt_tokens: 100,
    completion_tokens: 200,
    estimated_cost: cost,
  }
}

/** 统计响应按 group_by 给不同分组，避免渠道维度与模型维度串味。 */
function statsResponse(groupBy: string) {
  if (groupBy === 'channel') {
    return {
      granularity: 'hour',
      group_by: groupBy,
      items: [statsBucket('channel-a', 40, 2), statsBucket('channel-b', 60, 3)],
    }
  }
  if (groupBy === 'channel_model') {
    return {
      granularity: 'hour',
      group_by: groupBy,
      items: [
        statsBucket(
          `channel-a${PBR_CHANNEL_MODEL_SEPARATOR}deepseek-v4`,
          80,
          2
        ),
        statsBucket(
          `channel-b${PBR_CHANNEL_MODEL_SEPARATOR}deepseek-v4`,
          20,
          1
        ),
        statsBucket(
          `channel-b${PBR_CHANNEL_MODEL_SEPARATOR}gpt-4o-mini`,
          10,
          1
        ),
      ],
    }
  }
  return {
    granularity: 'hour',
    group_by: groupBy,
    items: [
      statsBucket('deepseek-v4', 100, 4),
      statsBucket('gpt-4o-mini', 50, 1),
    ],
  }
}

function modelStatusResponse() {
  return {
    from: BUCKET_TS,
    to: BUCKET_TS,
    items: [
      // 故意乱序：组件必须按 tokens_total 降序。
      {
        model: 'mystery-model',
        requests: 3,
        successes: 3,
        failures: 0,
        success_rate: 1,
        avg_total_ms: 0,
        avg_ttft_ms: 0,
        output_tps: 0,
        tokens_in: 1000,
        tokens_out: 500,
        tokens_total: 1_500,
        estimated_cost: 0.01,
      },
      {
        model: 'gpt-4o-mini',
        requests: 20,
        successes: 19,
        failures: 1,
        success_rate: 0.95,
        avg_total_ms: 1200,
        avg_ttft_ms: 300,
        output_tps: 22.5,
        tokens_in: 1_000_000,
        tokens_out: 555_400_000,
        tokens_total: 556_400_000,
        estimated_cost: 1.2,
      },
      {
        model: 'deepseek-v4',
        requests: 100,
        successes: 99,
        failures: 1,
        success_rate: 0.99,
        avg_total_ms: 840,
        avg_ttft_ms: 220,
        output_tps: 41.2,
        tokens_in: 1_000_000_000,
        tokens_out: 14_100_000_000,
        tokens_total: 15_100_000_000,
        estimated_cost: 12,
      },
    ],
  }
}

function mockApi(options?: { empty?: boolean; fail?: boolean }) {
  vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    const params = (config?.params ?? {}) as Record<string, unknown>
    calls.push({ url, params })
    if (options?.fail) throw new Error('Backend unavailable')
    if (url === '/api/model-status') {
      return {
        data: options?.empty
          ? { from: 0, to: 0, items: [] }
          : modelStatusResponse(),
      }
    }
    if (url === '/api/stats') {
      const groupBy = String(params.group_by ?? 'lane')
      return {
        data: options?.empty
          ? { granularity: 'hour', group_by: groupBy, items: [] }
          : statsResponse(groupBy),
      }
    }
    throw new Error(`Unexpected dashboard request: ${url}`)
  })
}

function renderOverview() {
  return render(
    <QueryClientProvider client={client}>
      <OverviewDashboard />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  calls = []
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})

describe('overview panel layout', () => {
  it('renders the time presets and the three cards', async () => {
    mockApi()
    renderOverview()

    expect(screen.getByRole('tab', { name: '24 hours' })).toHaveAttribute(
      'data-active'
    )
    for (const label of [
      'Today',
      'Last 3 days',
      'Last 7 days',
      'Last 30 days',
      'Custom range',
    ]) {
      expect(screen.getByRole('tab', { name: label })).toBeVisible()
    }

    expect(await screen.findByText('Model analytics')).toBeVisible()
    expect(screen.getByText('Token ranking')).toBeVisible()
    expect(screen.getByText('Service status')).toBeVisible()
  })

  it('shows converted cost, call count and model count with a period comparison', async () => {
    mockApi()
    renderOverview()

    expect(await screen.findByText('Converted cost')).toBeVisible()
    expect(screen.getByText('Call count')).toBeVisible()
    expect(screen.getByText('Model count')).toBeVisible()
    // 三个指标各有一条与上一等长窗口的环比。
    expect(screen.getAllByText('vs previous period')).toHaveLength(3)
    // 折算成本 = 4 + 1（元）。
    expect(screen.getByText('¥5.00')).toBeVisible()
  })

  it('re-queries every card with the new from/to when the preset changes', async () => {
    mockApi()
    const user = userEvent.setup()
    renderOverview()

    await screen.findByText('Model analytics')
    const firstFrom = Number(
      calls.find((call) => call.url === '/api/stats')?.params.from
    )
    await user.click(screen.getByRole('tab', { name: 'Today' }))

    const todayStart = startOfLocalDaySeconds(Math.floor(Date.now() / 1000))
    // 今日从本地自然日 00:00 起算，与滚动 24h 的口径确实不同。
    expect(todayStart).not.toBe(firstFrom)
    expect(calls.map((call) => Number(call.params.from))).toContain(todayStart)
    // 换档后所有卡片都以新窗口重新取数。
    const urls = new Set(
      calls
        .filter((call) => Number(call.params.from) === todayStart)
        .map((call) => call.url)
    )
    expect(urls).toEqual(new Set(['/api/stats', '/api/model-status']))
  })
})

describe('token ranking card', () => {
  it('lists top models by token total with B/M abbreviations', async () => {
    mockApi()
    renderOverview()

    const ranking = await screen.findByRole('list', { name: 'Token ranking' })
    const rows = within(ranking).getAllByRole('listitem')
    expect(rows).toHaveLength(3)

    expect(rows[0]).toHaveTextContent('deepseek-v4')
    expect(rows[0]).toHaveTextContent('15.1B')
    expect(rows[1]).toHaveTextContent('gpt-4o-mini')
    expect(rows[1]).toHaveTextContent('556.4M')
    expect(rows[2]).toHaveTextContent('mystery-model')
    expect(rows[2]).toHaveTextContent('1.5K')
  })

  it('shows the channel serving a model and omits it when unknown', async () => {
    mockApi()
    renderOverview()

    // deepseek-v4 的服务渠道取请求数最多的 channel-a（80 vs 20）。
    expect(await screen.findByText('Served by channel-a')).toBeVisible()
    expect(screen.getByText('Served by channel-b')).toBeVisible()
    // 没有渠道样本的模型不显示该行。
    expect(screen.getAllByText(/^Served by /)).toHaveLength(2)
  })
})

describe('service status card', () => {
  it('shows latency, throughput and a coloured success indicator per model', async () => {
    mockApi()
    renderOverview()

    const list = await screen.findByRole('list', { name: 'Service status' })
    const row = within(list).getByText('gpt-4o-mini').closest('li')
    expect(row).not.toBeNull()
    expect(row).toHaveTextContent('1.20s')
    expect(row).toHaveTextContent('22.5 tps')
    expect(row).toHaveTextContent('95.00%')
    // 成功率用彩色环指示，不是纯文字。
    expect(
      within(row as HTMLElement).getByRole('img', {
        name: 'Success rate 95.00%',
      })
    ).toBeVisible()

    // 空样本的延迟与吞吐量显示 - 而不是 0。
    const emptyRow = within(list).getByText('mystery-model').closest('li')
    expect(emptyRow).not.toBeNull()
    expect(within(emptyRow as HTMLElement).getAllByText('-')).toHaveLength(2)
  })

  it('scrolls inside the card instead of growing the page', async () => {
    mockApi()
    renderOverview()

    await screen.findByRole('list', { name: 'Service status' })
    const scroller = document.querySelector('.overflow-y-auto')
    expect(scroller).not.toBeNull()
    expect(scroller).toHaveClass('max-h-[300px]')
  })
})

describe('overview states', () => {
  it('shows empty copy instead of blank cards', async () => {
    mockApi({ empty: true })
    renderOverview()

    await waitFor(() =>
      expect(screen.getAllByText('No data in the selected range')).toHaveLength(
        3
      )
    )
  })

  it('shows a retryable error state', async () => {
    mockApi({ fail: true })
    renderOverview()

    expect(await screen.findByText('Model analytics')).toBeVisible()
    expect(
      (await screen.findAllByText('Failed to load')).length
    ).toBeGreaterThan(0)
    expect(
      screen.getAllByRole('button', { name: 'Retry' }).length
    ).toBeGreaterThan(0)
  })
})
