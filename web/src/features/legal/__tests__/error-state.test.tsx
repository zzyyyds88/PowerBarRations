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

import { LegalDocument } from '../legal-document'

vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: unknown }) => props.children,
}))

it('renders a retryable error instead of the "not configured" card', async () => {
  const fetchDocument = vi
    .fn<() => Promise<string>>()
    .mockRejectedValueOnce(new Error('offline'))
    .mockResolvedValue('Legal body')
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  render(
    <QueryClientProvider client={client}>
      <LegalDocument
        title='Privacy Policy'
        queryKey='privacy-policy-test'
        fetchDocument={fetchDocument}
        emptyMessage='Not configured'
      />
    </QueryClientProvider>
  )

  expect(await screen.findByText('Failed to load legal document')).toBeVisible()
  expect(screen.getByText('offline')).toBeVisible()
  expect(screen.queryByText('Not configured')).not.toBeInTheDocument()

  await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByText('Legal body')).toBeVisible()
})
