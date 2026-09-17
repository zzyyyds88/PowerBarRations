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
import { webcrypto } from 'node:crypto'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'

const { navigate } = vi.hoisted(() => ({ navigate: vi.fn() }))

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => navigate,
}))
vi.mock('@/hooks/use-system-config', () => ({
  useSystemConfig: () => ({ systemName: 'PowerBarRations' }),
}))
vi.mock('@/lib/pbr-auth', () => ({
  submitPBRSetup: vi.fn().mockResolvedValue({ warning: '' }),
}))

import { SetupWizard } from '../setup-wizard'

const EXPECTED = 'XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg='

beforeAll(() => {
  Object.defineProperty(globalThis, 'crypto', {
    configurable: true,
    value: webcrypto,
  })
})

afterEach(() => {
  cleanup()
  navigate.mockReset()
  localStorage.clear()
})

it('shows the one-time admin key after setup without storing it', async () => {
  const user = userEvent.setup()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <SetupWizard />
    </QueryClientProvider>
  )

  await user.type(screen.getByLabelText('Login password'), 'password')
  await user.type(screen.getByLabelText('Confirm password'), 'password')
  await user.click(screen.getByRole('button', { name: 'Initialize' }))

  const adminKey = await screen.findByLabelText('Admin key')
  expect(adminKey).toHaveValue(EXPECTED)
  expect(screen.getByText('How to recompute')).toBeVisible()
  expect(navigate).not.toHaveBeenCalled()

  const stored = Object.keys(localStorage)
    .map((key) => localStorage.getItem(key) ?? '')
    .join(' ')
  expect(stored).not.toContain(EXPECTED)

  await user.click(screen.getByRole('button', { name: 'Continue to console' }))
  expect(navigate).toHaveBeenCalledWith({ href: '/dashboard', replace: true })
})
