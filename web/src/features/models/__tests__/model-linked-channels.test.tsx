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
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { ModelLinkedChannels } from '../components/model-linked-channels'

const clients: QueryClient[] = []

function Page() {
  return <ModelLinkedChannels modelName='model1' nameRule={0} />
}

async function renderAssociation(channels: unknown[]) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/search') {
      return {
        data: {
          success: true,
          data: { items: channels, total: channels.length },
        },
      }
    }
    return { data: { success: true, data: { items: [] } } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const root = createRootRoute()
  const authenticated = createRoute({
    getParentRoute: () => root,
    id: '_authenticated',
  })
  const channelsRoute = createRoute({
    getParentRoute: () => authenticated,
    path: 'channels',
    component: Page,
  })
  const router = createRouter({
    routeTree: root.addChildren([authenticated.addChildren([channelsRoute])]),
    history: createMemoryHistory({ initialEntries: ['/channels'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

it('lists channels that declare the model and prefers the channel upstream price', async () => {
  await renderAssociation([
    {
      id: 1,
      name: 'Cheap upstream',
      type: 1,
      models: 'model1',
      model_mapping: null,
      setting: JSON.stringify({
        pbr_prices: [{ model: 'model1', input: 3, output: 4 }],
      }),
    },
    {
      id: 2,
      name: 'Default upstream',
      type: 1,
      models: 'model1,model-other',
      model_mapping: null,
      setting: '{}',
    },
    {
      id: 3,
      name: 'Unrelated',
      type: 1,
      models: 'other-model',
      model_mapping: null,
      setting: '{}',
    },
  ])

  expect(await screen.findByText('Cheap upstream')).toBeVisible()
  expect(screen.getByText('Default upstream')).toBeVisible()
  expect(screen.queryByText('Unrelated')).not.toBeInTheDocument()

  // 渠道价（单层单价）：配了价的渠道显示金额，未配价的渠道显示"未配置"。
  expect(screen.getAllByText(/¥3 \/ ¥4/).length).toBeGreaterThan(0)
  expect(screen.getByText('Channel price')).toBeVisible()
  expect(screen.getAllByText('Not configured').length).toBeGreaterThan(0)

  expect(screen.getAllByRole('button', { name: 'Edit channel' })).toHaveLength(
    2
  )
})

it('shows an empty state when no channel declares the model', async () => {
  await renderAssociation([])

  expect(
    await screen.findByText('No channel serves this model yet.')
  ).toBeVisible()
})
