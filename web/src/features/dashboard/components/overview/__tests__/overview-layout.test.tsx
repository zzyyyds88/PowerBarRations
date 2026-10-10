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
import { MetricStrip } from '../metric-strip'
import { OverviewDashboard } from '../overview-dashboard'

type RecordedCall = { url: string; params: Record<string, unknown> }

let client: QueryClient
let calls: RecordedCall[]

const BUCKET_TS = 1_700_000_000

function statsBucket(
  group: string,
  requests: number,
  cost: number,
  promptTokens = 100,
  completionTokens = 200
) {
  return {
    bucket_ts: BUCKET_TS,
    group,
    requests,
    successes: requests,
    prompt_tokens: promptTokens,
    completion_tokens: completionTokens,
    estimated_cost: cost,
  }
}

/**
 * 统计响应按 group_by 给不同分组，避免渠道维度与模型维度串味。
 *
 * 模型维度（指标条与「模型数据分析」共用）合计：请求 150、成本 ¥5、
 * token 3,000,000 —— 24h 窗口下 RPM = 150/1440 = 0.1、TPM = 2,083 → `2.1K`。
 */
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
      statsBucket('deepseek-v4', 100, 4, 1_000_000, 1_000_000),
      statsBucket('gpt-4o-mini', 50, 1, 500_000, 500_000),
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
      if (options?.empty) {
        return {
          data: { granularity: 'hour', group_by: groupBy, items: [] },
        }
      }
      // 环比基准窗口（结束于当前窗口起点）给一半的量：RPM/TPM 与三个合计
      // 因此都得到可读的正环比，而不是"两个窗口一模一样"的 0%。
      if (isPreviousWindow(params)) {
        return { data: halvedResponse(groupBy) }
      }
      return { data: statsResponse(groupBy) }
    }
    throw new Error(`Unexpected dashboard request: ${url}`)
  })
}

/** 上一等长窗口的请求终点 = 当前窗口的起点（≈ now - 窗口长度）。 */
function isPreviousWindow(params: Record<string, unknown>): boolean {
  const to = Number(params.to)
  if (!Number.isFinite(to)) return false
  return to < Math.floor(Date.now() / 1000) - 3600
}

/** 上一窗口的响应：各组的量减半，其余形状不变。 */
function halvedResponse(groupBy: string) {
  return {
    ...statsResponse(groupBy),
    items: statsResponse(groupBy).items.map((item) => ({
      ...item,
      requests: Math.round(item.requests / 2),
      successes: Math.round(item.successes / 2),
      prompt_tokens: Math.round(item.prompt_tokens / 2),
      completion_tokens: Math.round(item.completion_tokens / 2),
      estimated_cost: item.estimated_cost / 2,
    })),
  }
}

function renderOverview() {
  return render(
    <QueryClientProvider client={client}>
      <OverviewDashboard />
    </QueryClientProvider>
  )
}

/** 指标条一行五格；按格取标签/值/口径副标题。 */
function findMetricStrip(): HTMLElement {
  const strip = document.querySelector('[data-slot="metric-strip"]')
  expect(strip).not.toBeNull()
  return strip as HTMLElement
}

function findMetricTile(label: string): HTMLElement {
  const tile = within(findMetricStrip())
    .getByText(label)
    .closest('[data-slot="metric-tile"]')
  expect(tile).not.toBeNull()
  return tile as HTMLElement
}

/** 卡片标题右侧的窗口汇总（`Total: …`）；数据未到位时不渲染，因此要等。 */
function findPanelTotals(): HTMLElement[] {
  return [
    ...document.querySelectorAll<HTMLElement>('[data-slot="panel-total"]'),
  ]
}
async function waitForPanelTotals(count: number): Promise<HTMLElement[]> {
  await waitFor(() => expect(findPanelTotals()).toHaveLength(count))
  return findPanelTotals()
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

describe('metric strip', () => {
  it('renders one row of five tiles with labels, values and captions', async () => {
    mockApi()
    renderOverview()

    // 等合计到位再断言：加载中显示骨架而不是 `0`。
    await waitFor(() =>
      expect(findMetricTile('Call count')).toHaveTextContent('150')
    )

    // 五格 = 折算成本 / 调用次数 / Token 总量 / RPM / TPM。
    const strip = findMetricStrip()
    for (const label of [
      'Converted cost',
      'Call count',
      'Total tokens',
      'RPM',
      'TPM',
    ]) {
      expect(within(strip).getByText(label)).toBeVisible()
    }

    // 口径副标题说明每一格算的是什么。
    for (const caption of [
      'Statistical cost',
      'Statistical count',
      'Statistical tokens',
      'Requests per minute',
      'Tokens per minute',
    ]) {
      expect(within(strip).getByText(caption)).toBeVisible()
    }

    // 合计口径：¥5（4+1）、150 次、3,000,000 token。
    expect(findMetricTile('Converted cost')).toHaveTextContent('¥5.00')
    expect(findMetricTile('Call count')).toHaveTextContent('150')
    expect(findMetricTile('Total tokens')).toHaveTextContent('3.0M')
  })

  it('keeps a previous-period comparison on every one of the five tiles', async () => {
    mockApi()
    renderOverview()

    await waitFor(() =>
      expect(findMetricTile('Call count')).toHaveTextContent('150')
    )

    // 五格各有与上一等长窗口的环比；上一窗口的量是一半，因此环绕比是 +100.0%。
    const tiles = document.querySelectorAll('[data-slot="metric-tile"]')
    expect(tiles).toHaveLength(5)
    for (const tile of tiles) {
      expect(tile).toHaveTextContent('vs previous period')
      expect(tile).toHaveTextContent('+100.0%')
    }
  })

  it('lays the five tiles out as one row on wide screens and wraps when narrow', async () => {
    mockApi()
    renderOverview()

    await waitFor(() => expect(findMetricStrip()).toBeVisible())
    const strip = findMetricStrip()
    // 窄屏换行：1 列 → sm 2 列 → lg 5 列；间距走密度 token。
    expect(strip).toHaveClass('grid-cols-1')
    expect(strip).toHaveClass('sm:grid-cols-2')
    expect(strip).toHaveClass('lg:grid-cols-5')
    expect(strip).toHaveClass('gap-(--density-grid)')
  })

  it('derives RPM and TPM from the window totals and the window length', async () => {
    mockApi()
    renderOverview()

    // RPM = 150 次 / 1440 分钟 = 0.104 → 一位小数；TPM = 3,000,000 / 1440 = 2,083 → 2.1K。
    await waitFor(() => expect(findMetricTile('RPM')).toHaveTextContent('0.1'))
    expect(findMetricTile('TPM')).toHaveTextContent('2.1K')
    // 速率是可见数字（不是空串）。
    expect(findMetricStrip()).toHaveTextContent(/\d/)
  })

  it('shows a zero rate instead of NaN when the window has no length', async () => {
    mockApi()
    // 异常窗口（from === to）下窗口分钟数为 0；除零必须收敛成 `0.0` / `0`。
    const degenerateRange = {
      preset: 'custom' as const,
      from: 1_700_000_000,
      to: 1_700_000_000,
      granularity: 'hour' as const,
      previousFrom: 1_700_000_000,
      previousTo: 1_700_000_000,
    }
    render(
      <QueryClientProvider client={client}>
        <MetricStrip range={degenerateRange} />
      </QueryClientProvider>
    )

    await waitFor(() => expect(findMetricTile('RPM')).toHaveTextContent('0.0'))
    expect(findMetricTile('TPM')).toHaveTextContent('0')
    expect(findMetricTile('RPM')).not.toHaveTextContent('NaN')
    expect(findMetricTile('TPM')).not.toHaveTextContent('Infinity')
  })

  it('reuses the model query of the analytics card instead of fetching twice', async () => {
    mockApi()
    renderOverview()

    await screen.findByText('Model analytics')
    const firstFrom = Number(
      calls.find((call) => call.url === '/api/stats')?.params.from
    )
    // 同一窗口、同一 group_by 的模型聚合只允许一次请求（指标条与卡片共用查询键）。
    const modelCalls = calls.filter(
      (call) =>
        call.url === '/api/stats' &&
        call.params.group_by === 'model' &&
        Number(call.params.from) === firstFrom
    )
    expect(modelCalls).toHaveLength(1)
  })
})

describe('panel header totals', () => {
  it('shows the request count total for the default trend tab', async () => {
    mockApi()
    renderOverview()

    // 「模型数据分析」标题右侧默认（趋势）挂当前窗口的调用次数合计。
    const totals = await waitForPanelTotals(3)
    expect(totals[0]).toHaveTextContent('150')
    // 汇总挂在标题行右侧：与标题同一个 `justify-between` 行，且排在标题之后。
    const row = totals[0].parentElement as HTMLElement
    expect(row).toHaveClass('justify-between')
    expect(row).toHaveTextContent('Model analytics')
    expect(row.lastElementChild).toBe(totals[0])
    expect(totals[0].closest('[data-slot="panel-header"]')).not.toBeNull()
  })

  it('switches the analytics total to the cost amount on the consumption tab', async () => {
    mockApi()
    const user = userEvent.setup()
    renderOverview()

    const [before] = await waitForPanelTotals(3)
    expect(before).toHaveTextContent('150')

    await user.click(
      screen.getByRole('tab', { name: 'Consumption distribution' })
    )

    // 消耗 tab 的主指标是金额，右侧汇总随之变成当前窗口的折算成本。
    await waitFor(() => expect(findPanelTotals()[0]).toHaveTextContent('¥5.00'))
  })

  it('mounts a total on every card: token sum for ranking, model count for status', async () => {
    mockApi()
    renderOverview()

    const totals = await waitForPanelTotals(3)
    // 排行榜挂窗口内全部模型的 token 总量（不只是 Top 10 的和）。
    expect(totals[1]).toHaveTextContent('15.7B')
    // 服务状态挂窗口内的模型数。
    expect(totals[2]).toHaveTextContent('3')
  })

  it('omits the total entirely while the data is still loading', async () => {
    mockApi()
    const { container } = renderOverview()

    // 加载中渲染骨架，标题右侧不得出现伪造的 `Total: 0`。
    expect(container.querySelector('[data-slot="skeleton"]')).not.toBeNull()
    expect(findPanelTotals()).toHaveLength(0)
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

  it('keeps the header sticky while scrolling inside the card', async () => {
    mockApi()
    renderOverview()

    await screen.findByRole('list', { name: 'Service status' })
    const scroller = document.querySelector(
      '.max-h-\\[300px\\].overflow-y-auto'
    )
    expect(scroller).not.toBeNull()
    // 表头在卡内滚动时吸顶（sticky），列语义不跟着滚走。
    expect(
      (scroller as HTMLElement).querySelector('.sticky.top-0')
    ).not.toBeNull()
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
