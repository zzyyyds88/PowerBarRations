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
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  PBR_CHANNEL_MODEL_SEPARATOR,
  type PBRStatBucket,
} from '../../../pbr-stats-api'
import { CostDashboard, PbrAnalyticsDashboard } from '../cost-dashboard'

const ITEMS: PBRStatBucket[] = [
  {
    bucket_ts: 1_700_000_000,
    group: `ch-a${PBR_CHANNEL_MODEL_SEPARATOR}model-a`,
    requests: 2,
    successes: 2,
    prompt_tokens: 100,
    completion_tokens: 50,
    estimated_cost: 0.9,
  },
  {
    bucket_ts: 1_700_000_000,
    group: `ch-b${PBR_CHANNEL_MODEL_SEPARATOR}model-b`,
    requests: 3,
    successes: 1,
    prompt_tokens: 200,
    completion_tokens: 100,
    estimated_cost: 0.3,
  },
]

let client: QueryClient

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
})

function mockStats(items: PBRStatBucket[]) {
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: { granularity: 'hour', group_by: 'channel_model', items },
  }))
}

function renderDashboard(node: ReactNode) {
  return render(
    <QueryClientProvider client={client}>{node}</QueryClientProvider>
  )
}

describe('cost dashboard channel x model dimension', () => {
  it('renders channel and model as split columns sorted by cost descending', async () => {
    mockStats(ITEMS)
    renderDashboard(
      <PbrAnalyticsDashboard
        defaultGroupBy='channel_model'
        groupByOptions={['channel_model']}
      />
    )

    expect(await screen.findByText('ch-a')).toBeVisible()
    expect(screen.getByRole('columnheader', { name: 'Channel' })).toBeVisible()
    expect(screen.getByRole('columnheader', { name: 'Model' })).toBeVisible()
    expect(screen.getByRole('columnheader', { name: 'Requests' })).toBeVisible()
    expect(
      screen.getByRole('columnheader', { name: 'Token count' })
    ).toBeVisible()
    expect(
      screen.getByRole('columnheader', { name: 'Upstream spend' })
    ).toBeVisible()

    const rows = screen.getAllByRole('row')
    expect(within(rows[1]).getByText('ch-a')).toBeVisible()
    expect(within(rows[1]).getByText('model-a')).toBeVisible()
    expect(within(rows[1]).getByText('¥0.90')).toBeVisible()
    expect(within(rows[2]).getByText('ch-b')).toBeVisible()
    expect(within(rows[2]).getByText('model-b')).toBeVisible()
  })

  it('shows stat totals equal to the sum of the buckets', async () => {
    mockStats(ITEMS)
    renderDashboard(
      <PbrAnalyticsDashboard
        defaultGroupBy='channel_model'
        groupByOptions={['channel_model']}
      />
    )

    expect(await screen.findByText('ch-a')).toBeVisible()
    expect(screen.getByText('¥1.20')).toBeVisible()
    expect(screen.getByText('5')).toBeVisible()
    expect(screen.getByText('450')).toBeVisible()
  })

  it('offers the channel x model option and switches to split columns', async () => {
    mockStats(ITEMS)
    const user = userEvent.setup()
    renderDashboard(<CostDashboard />)

    await user.click(await screen.findByRole('combobox'))
    await user.click(
      await screen.findByRole('option', { name: 'Channel × Model' })
    )

    expect(
      await screen.findByRole('columnheader', { name: 'Channel' })
    ).toBeVisible()
    expect(screen.getByRole('columnheader', { name: 'Model' })).toBeVisible()
  })

  it('shows the empty-state copy when there is no channel-model data', async () => {
    mockStats([])
    renderDashboard(
      <PbrAnalyticsDashboard
        defaultGroupBy='channel_model'
        groupByOptions={['channel_model']}
      />
    )

    expect(await screen.findByText('No cost data yet')).toBeVisible()
    expect(
      screen.getByText(
        'Configure upstream unit prices in the channel to see cost accounting here.'
      )
    ).toBeVisible()
  })
})
