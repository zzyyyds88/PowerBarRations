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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'

import { About } from '..'
import { getAboutContent } from '../api'

vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: unknown }) => props.children,
}))

vi.mock('../api', () => ({
  getAboutContent: vi.fn(),
}))

function renderAbout() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <About />
    </QueryClientProvider>
  )
}

it('renders a retryable error instead of the "not configured" empty state', async () => {
  vi.mocked(getAboutContent)
    .mockRejectedValueOnce(new Error('offline'))
    .mockResolvedValue('# Hello about')

  renderAbout()

  expect(await screen.findByText('Failed to load about content')).toBeVisible()
  expect(screen.getByText('offline')).toBeVisible()
  expect(screen.queryByText('No About Content Set')).not.toBeInTheDocument()

  await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByText('Hello about')).toBeVisible()
})
