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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { ResilienceSettingsSection } from '../resilience-settings-section'
import { WebhookDeliveriesSection } from '../webhook-deliveries-section'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), put: vi.fn() },
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), info: vi.fn() } }))

const mockedGet = vi.mocked(api.get)
const mockedPut = vi.mocked(api.put)

const renderSection = (node: React.ReactNode) => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>{node}</QueryClientProvider>
  )
}

describe('WebhookDeliveriesSection', () => {
  beforeEach(() => vi.clearAllMocks())

  test('shows failed deliveries with the receiver error and status', async () => {
    mockedGet.mockResolvedValue({
      data: {
        items: [
          {
            id: 1,
            ts: 1789600000,
            target: 'notify',
            event_type: 'circuit_open',
            lane: 'lane-a',
            member: '1:model-x',
            status: 'failed',
            http_status: 0,
            error: 'context deadline exceeded',
            attempt: 4,
          },
        ],
        dropped_events: 2,
      },
    } as never)

    renderSection(<WebhookDeliveriesSection />)

    expect(await screen.findByText('circuit_open')).toBeVisible()
    expect(screen.getByText('lane-a / 1:model-x')).toBeVisible()
    expect(screen.getByText('context deadline exceeded')).toBeVisible()
    expect(screen.getByText(/Dropped events: 2/)).toBeVisible()
  })
})

describe('ResilienceSettingsSection', () => {
  beforeEach(() => vi.clearAllMocks())

  test('loads circuit options and saves the edited values', async () => {
    mockedGet.mockResolvedValue({
      data: {
        circuit_failure_threshold: 5,
        circuit_open_seconds: 0,
        circuit_max_open_seconds: 1800,
        circuit_rolling_min_samples: 8,
        circuit_rolling_failure_rate: 0.8,
        circuit_half_open_successes: 2,
      },
    } as never)
    mockedPut.mockResolvedValue({ data: {} } as never)

    renderSection(<ResilienceSettingsSection />)

    const inputs = await screen.findAllByRole('spinbutton')
    await waitFor(() => expect(inputs[0]).toHaveValue(5))

    fireEvent.change(inputs[0], { target: { value: '6' } })
    fireEvent.click(
      screen.getByRole('button', { name: 'Save resilience settings' })
    )

    await waitFor(() => {
      expect(mockedPut).toHaveBeenCalledWith(
        '/api/v1/system/options',
        expect.objectContaining({ circuit_failure_threshold: 6 })
      )
    })
  })
})
