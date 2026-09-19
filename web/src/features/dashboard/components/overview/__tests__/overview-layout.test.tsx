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
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { OverviewDashboard } from '../overview-dashboard'

let client: QueryClient

beforeEach(() => {
  window.localStorage.clear()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'dashboard-user',
    role: 1,
    quota: 1000000,
    used_quota: 1000,
    request_count: 1,
  })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    switch (url) {
      case '/api/keys':
        return {
          data: {
            items: [
              {
                id: 1,
                name: 'App key',
                enabled: true,
                key_prefix: 'pbr-masked',
                lane_policy: { mode: 'all', allow_lanes: [], deny_lanes: [] },
                ip_allowlist: [],
                expires_at: null,
                created_at: '2026-09-15T16:47:00Z',
                last_used_at: null,
              },
            ],
          },
        }
      case '/api/models':
        return {
          data: {
            items: [
              { model: 'gpt-4o-mini', source: 'implicit', member_count: 1 },
            ],
          },
        }
      case '/api/stats':
        return {
          data: {
            granularity: 'hour',
            group_by: 'lane',
            items: [
              {
                bucket_ts: Math.floor(Date.now() / 1000 / 3600) * 3600,
                group: 'gpt-4o-mini',
                requests: 3,
                successes: 3,
                prompt_tokens: 10,
                completion_tokens: 5,
                estimated_cost: 0.01,
              },
            ],
          },
        }
      default:
        throw new Error(`Unexpected dashboard request: ${url}`)
    }
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  window.localStorage.clear()
})

async function renderOverview() {
  const router = createRouter({
    routeTree: createRootRoute({ component: OverviewDashboard }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('overview layout', () => {
  it('renders usage summary and the first-request preview without the setup guide', async () => {
    await renderOverview()

    expect(await screen.findByText('Usage at a glance')).toBeVisible()
    expect(await screen.findByText('First API request')).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Setup guide' })
    ).not.toBeInTheDocument()
    expect(screen.queryByText('Get started')).not.toBeInTheDocument()
    expect(screen.queryByText(/Setup progress:/)).not.toBeInTheDocument()
  })

  it('does not render recommended quick actions', async () => {
    await renderOverview()

    expect(await screen.findByText('First API request')).toBeVisible()
    expect(screen.queryByText('Recommended actions')).not.toBeInTheDocument()
    expect(
      screen.queryByText('Keep the platform ready')
    ).not.toBeInTheDocument()
  })

  it('shows a retryable failure instead of a stuck loading or missing-key signal', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/keys' || url === '/api/models') {
        throw new Error('Backend unavailable')
      }
      switch (url) {
        case '/api/stats':
          return { data: { granularity: 'hour', group_by: 'lane', items: [] } }
        default:
          throw new Error(`Unexpected dashboard request: ${url}`)
      }
    })

    await renderOverview()

    expect(await screen.findByText('First API request')).toBeVisible()
    expect(await screen.findAllByText('Failed to load')).toHaveLength(2)
    expect(screen.queryByText('Needs API key')).not.toBeInTheDocument()
    expect(screen.queryByText('Loading')).not.toBeInTheDocument()
  })
})
