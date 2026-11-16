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
import { QueryClient } from '@tanstack/react-query'
import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'

import { renderPage } from '@/test-utils/render-page'

import type { UsageLog } from '../../data/schema'
import { fetchLogsByCategory } from '../../lib/utils'
import { UsageLogsProvider } from '../usage-logs-provider'
import { UsageLogsTable } from '../usage-logs-table'

vi.mock('../../lib/utils', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/utils')>()
  return {
    ...actual,
    fetchLogsByCategory: vi.fn(),
  }
})

function makeLog(
  id: number,
  model: string,
  lane: string,
  errorSummary: string
): UsageLog {
  return {
    id,
    user_id: 0,
    created_at: 1_760_000_000,
    type: 2,
    content: errorSummary,
    username: '',
    token_name: 'key-a',
    model_name: model,
    quota: 0,
    prompt_tokens: 10,
    completion_tokens: 5,
    use_time: 1,
    is_stream: false,
    channel: 1,
    channel_name: 'channel-a',
    token_id: 1,
    group: '',
    ip: '',
    other: JSON.stringify({
      admin_info: { use_channel: ['channel-a'] },
      pbr: {
        lane,
        route_source: 'explicit',
        upstream_model: model,
        error_kind: '',
        error_summary: errorSummary,
        http_status: 200,
        total_ms: 100,
        estimated_cost: 0.1,
        attempts: [],
        total_attempts: 1,
        inbound_format: 'openai',
        success: true,
      },
    }),
    request_id: '',
    upstream_request_id: '',
  }
}

const logA = makeLog(1, 'model-a', 'lane-a', 'detail-a')
const logB = makeLog(2, 'model-b', 'lane-b', 'detail-b')
const logC = makeLog(3, 'model-c', 'lane-c', 'detail-c')

beforeEach(() => {
  vi.mocked(fetchLogsByCategory).mockReset()
})

it('keeps the expanded request expanded when tail refetches reorder rows', async () => {
  const fetchLogs = vi.mocked(fetchLogsByCategory)
  fetchLogs
    .mockResolvedValueOnce({
      items: [logA, logB],
      total: 2,
      page: 1,
      page_size: 20,
    })
    .mockResolvedValue({
      items: [logC, logA, logB],
      total: 3,
      page: 1,
      page_size: 20,
    })

  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  renderPage(
    <UsageLogsProvider>
      <UsageLogsTable logCategory='pbr' />
    </UsageLogsProvider>,
    { client, path: '/logs/requests?page=1' }
  )

  expect(await screen.findByText('model-a')).toBeVisible()
  const user = userEvent.setup()
  await user.click(
    within(screen.getByRole('table')).getAllByRole('button', {
      name: 'Expand',
    })[0]
  )

  expect(await screen.findByText('Request Details')).toBeVisible()
  expect(screen.getByText('detail-a')).toBeVisible()

  await act(async () => {
    await client.invalidateQueries({ queryKey: ['logs'] })
  })

  await waitFor(() => expect(screen.getByText('model-c')).toBeVisible())
  // The detail row must still belong to request id 1, not to whatever row is
  // now at the same index.
  expect(screen.getByText('Request Details')).toBeVisible()
  expect(screen.getByText('detail-a')).toBeVisible()
  expect(screen.queryByText('detail-c')).not.toBeInTheDocument()
})

it('offers Clear filters when a filtered request list is empty', async () => {
  vi.mocked(fetchLogsByCategory).mockResolvedValue({
    items: [],
    total: 0,
    page: 1,
    page_size: 20,
  })

  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const { router } = renderPage(
    <UsageLogsProvider>
      <UsageLogsTable logCategory='pbr' />
    </UsageLogsProvider>,
    { client, path: '/logs/requests?page=1&lane=lane-a' }
  )

  const clear = await screen.findByRole('button', { name: 'Clear filters' })
  await userEvent.click(clear)

  await waitFor(() =>
    expect(router.state.location.search).not.toHaveProperty('lane')
  )
})
