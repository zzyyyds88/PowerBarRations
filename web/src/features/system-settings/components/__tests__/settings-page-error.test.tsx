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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type React from 'react'
import { expect, it, vi } from 'vitest'

import { useSystemOptions } from '../../hooks/use-system-options'
import { SettingsPage } from '../settings-page'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useParams: () => ({ section: 'general' }),
    useLocation: (options?: {
      select?: (location: { pathname: string }) => unknown
    }) => {
      const location = { pathname: '/system-settings/site/general' }
      return options?.select ? options.select(location) : location
    },
    Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
      <a href={to}>{children}</a>
    ),
  }
})

vi.mock('../../hooks/use-system-options', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('../../hooks/use-system-options')>()
  return {
    ...actual,
    useSystemOptions: vi.fn(),
  }
})

function renderSettingsPage() {
  return render(
    <SettingsPage
      routePath='/system-settings/$section'
      defaultSettings={{ log_retention_days: 30 }}
      defaultSection='general'
      getSectionContent={() => <div>Settings body</div>}
      getSectionMeta={() => ({ titleKey: 'General' })}
    />
  )
}

it('renders a retryable error instead of the default settings form', async () => {
  const refetch = vi.fn()
  vi.mocked(useSystemOptions).mockReturnValue({
    data: undefined,
    isLoading: false,
    isError: true,
    error: new Error('offline'),
    refetch,
  } as never)

  renderSettingsPage()

  expect(screen.getByText('Failed to load settings')).toBeVisible()
  expect(screen.getByText('offline')).toBeVisible()
  expect(screen.queryByText('Settings body')).not.toBeInTheDocument()

  await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(refetch).toHaveBeenCalledTimes(1)
})

it('renders skeletons while the options request is in flight', () => {
  vi.mocked(useSystemOptions).mockReturnValue({
    data: undefined,
    isLoading: true,
    isError: false,
    error: null,
    refetch: vi.fn(),
  } as never)

  const { container } = renderSettingsPage()

  expect(
    screen.getByRole('status', { name: 'Loading settings...' })
  ).toBeVisible()
  expect(
    container.querySelectorAll('[data-slot="skeleton"]').length
  ).toBeGreaterThan(0)
  expect(screen.queryByText('Settings body')).not.toBeInTheDocument()
})
