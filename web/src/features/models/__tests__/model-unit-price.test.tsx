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
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { ModelUnitPriceCell } from '../components/model-unit-price-cell'

// design-v1 §16.9#7：模型页「上游单价」列展示**渠道上游单价**（单层单价）——
// 多渠道不同价时显示区间；没有渠道价即"未配置"（不折算），没有全局默认价。
const clients: QueryClient[] = []

function renderCell(channels: unknown[]) {
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
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <ModelUnitPriceCell modelName='model1' nameRule={0} />
    </QueryClientProvider>
  )
}

function channel(id: number, name: string, prices: unknown[]) {
  return {
    id,
    name,
    type: 1,
    models: 'model1',
    model_mapping: null,
    setting: JSON.stringify({ pbr_prices: prices }),
  }
}

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

it('prefers channel prices and shows the range across channels', async () => {
  renderCell([
    channel(1, 'A', [{ model: 'model1', input: 3, output: 4 }]),
    channel(2, 'B', [{ model: 'model1', input: 5, output: 9 }]),
  ])

  expect(await screen.findByText('¥3–5 / ¥4–9')).toBeVisible()
  expect(screen.getByText('Channel price')).toBeVisible()
})

it('shows a single channel price directly', async () => {
  renderCell([channel(1, 'A', [{ model: 'model1', input: 1, output: 2 }])])

  expect(await screen.findByText('¥1 / ¥2')).toBeVisible()
  expect(screen.getByText('Channel price')).toBeVisible()
})

it('shows Not configured when no channel sets a price', async () => {
  renderCell([channel(1, 'A', [])])

  expect(await screen.findByText('Not configured')).toBeVisible()
})

it('ignores channels that do not serve the model', async () => {
  renderCell([
    {
      id: 9,
      name: 'Unrelated',
      type: 1,
      models: 'other-model',
      model_mapping: null,
      setting: JSON.stringify({
        pbr_prices: [{ model: 'other-model', input: 99, output: 99 }],
      }),
    },
  ])

  expect(await screen.findByText('Not configured')).toBeVisible()
})
