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
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import {
  act,
  cleanup,
  render,
  screen,
  within,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { Toaster, toast } from 'sonner'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { apiKeySchema, type ApiKey } from '../../types'
import { useApiKeysColumns } from '../api-keys-columns'
import { ApiKeysProvider } from '../api-keys-provider'
import { ApiKeysTable } from '../api-keys-table'

const now = 1_700_000_000_000
const key = apiKeySchema.parse({
  id: 7,
  name: 'production',
  key: 'demo********1234',
  status: 1,
  cost: 0,
  expired_time: -1,
  created_time: 0,
  accessed_time: 0,
  model_limits_enabled: false,
})
const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})
const clients: QueryClient[] = []

function CostTable(props: { apiKey: ApiKey }) {
  const columns = useApiKeysColumns(now).filter(
    (column) => column.id === 'cost'
  )
  // eslint-disable-next-line react/incompatible-library -- test fixture only
  const table = useReactTable({
    columns,
    data: [props.apiKey],
    getCoreRowModel: getCoreRowModel(),
  })
  return (
    <table>
      <thead>
        {table.getHeaderGroups().map((group) => (
          <tr key={group.id}>
            {group.headers.map((header) => (
              <th key={header.id}>
                {flexRender(
                  header.column.columnDef.header,
                  header.getContext()
                )}
              </th>
            ))}
          </tr>
        ))}
      </thead>
      <tbody>
        {table.getRowModel().rows.map((row) => (
          <tr key={row.id}>
            {row.getVisibleCells().map((cell) => (
              <td key={cell.id}>
                {flexRender(cell.column.columnDef.cell, cell.getContext())}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function renderCost(apiKey: ApiKey = key) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, enabled: false } },
  })
  clients.push(client)
  return render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <CostTable apiKey={apiKey} />
      </QueryClientProvider>
    </I18nextProvider>
  )
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data: {} } })
})
afterEach(() => {
  cleanup()
  toast.dismiss()
  localStorage.clear()
  clients.splice(0).forEach((client) => client.clear())
})

it('shows the consumed amount under a single Consumed (¥) header', () => {
  renderCost({ ...key, cost: 120.5 })
  expect(
    screen.getByRole('columnheader', { name: 'Consumed (¥)' })
  ).toBeInTheDocument()
  const cell = screen.getByText('¥120.50')
  expect(cell).toHaveClass('tabular-nums')
  expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
})

it('renders zero and sub-cent consumed amounts without quota phrasing', () => {
  renderCost({ ...key, cost: 0 })
  expect(screen.getByText('¥0')).toBeVisible()
  cleanup()
  renderCost({ ...key, cost: 0.0042 })
  expect(screen.getByText('¥0.0042')).toBeVisible()
  expect(screen.queryByText(/quota/i)).not.toBeInTheDocument()
})

it('shows the read-only cost returned by GET /api/keys', async () => {
  await renderKeysPage({ cost: 42.75 })
  expect(screen.getByRole('cell', { name: '¥42.75' })).toBeInTheDocument()
})

it('does not render any quota, wallet or subscription wording on the keys page', async () => {
  await renderKeysPage()
  for (const pattern of [/quota/i, /wallet/i, /subscription/i, /remaining/i]) {
    expect(screen.queryByText(pattern)).not.toBeInTheDocument()
  }
})

function KeysPage() {
  return (
    <ApiKeysProvider>
      <ApiKeysTable />
      <Toaster />
    </ApiKeysProvider>
  )
}

type PbrKeyOverrides = {
  id?: number
  name?: string
  enabled?: boolean
  key?: string | null
  key_prefix?: string
  lane_policy?: {
    mode?: string
    allow_lanes?: string[]
    deny_lanes?: string[]
  }
  ip_allowlist?: string[]
  expires_at?: string | null
  last_used_at?: string | null
  cost?: number
}

async function renderKeysPage(overrides: PbrKeyOverrides = {}) {
  const pbrKey = {
    id: 7,
    name: 'production',
    enabled: true,
    key: 'pbr-existing-secret',
    key_prefix: 'pbr-abcd1234',
    lane_policy: { mode: 'all', allow_lanes: [], deny_lanes: [] },
    ip_allowlist: [],
    expires_at: null,
    created_at: '2026-09-15T16:47:00Z',
    updated_at: '2026-09-15T16:47:00Z',
    last_used_at: null,
    ...overrides,
  }
  vi.mocked(api.get).mockImplementation(async (url) => {
    if (url === '/api/keys') {
      return { data: { items: [pbrKey], next_cursor: null } }
    }
    if (url.startsWith('/api/keys/')) {
      return { data: pbrKey }
    }
    return { data: { success: true, data: {} } }
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { ...pbrKey, key: 'pbr-fake-key-for-test-only' },
  })
  const put = vi.spyOn(api, 'put').mockResolvedValue({ data: pbrKey })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], {})
  clients.push(client)
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const keysRoute = createRoute({
    getParentRoute: () => auth,
    path: 'keys/',
    component: KeysPage,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([keysRoute])]),
    history: createMemoryHistory({ initialEntries: ['/keys/'] }),
  })
  await router.load()
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  await screen.findByText(pbrKey.name)
  return { post, put }
}

it('lists PBR client keys with the columns the contract exposes', async () => {
  await renderKeysPage()
  for (const name of ['Name', 'API Key', 'Models', 'IP Restriction']) {
    expect(screen.getByRole('columnheader', { name })).toBeInTheDocument()
  }
  expect(
    screen.getByRole('cell', { name: /Created.*Last Used/ })
  ).toBeInTheDocument()
  expect(screen.getByText('Enabled')).toBeInTheDocument()
})

it('toggles enabled through PUT /api/keys/{name}', async () => {
  const { put, post } = await renderKeysPage()
  const user = userEvent.setup()
  const button = screen.getByRole('button', { name: 'Disable' })
  act(() => button.focus())
  await user.keyboard('{Enter}')
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/keys/production', { enabled: false })
  )
  expect(post).not.toHaveBeenCalled()
})

it('Copy Key uses the stored plaintext without rotating', async () => {
  const user = userEvent.setup()
  const { post } = await renderKeysPage()
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  await user.click(screen.getByRole('button', { name: 'Open menu' }))
  expect(post).not.toHaveBeenCalled()
  await user.click(screen.getByRole('menuitem', { name: 'Copy Key' }))
  await waitFor(() => expect(copy).toHaveBeenCalledWith('pbr-existing-secret'))
})

it('Copy Key for a legacy key requires rotation confirmation', async () => {
  const user = userEvent.setup()
  const { post } = await renderKeysPage({ key: null })
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  await user.click(screen.getByRole('button', { name: 'Open menu' }))
  await user.click(screen.getByRole('menuitem', { name: 'Copy Key' }))
  const dialog = await screen.findByRole('alertdialog')
  await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  expect(post).not.toHaveBeenCalled()
  expect(copy).not.toHaveBeenCalled()
})

it('shows model and IP restrictions in the mobile card details', async () => {
  const matchMedia = window.matchMedia
  vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
    ...matchMedia(query),
    matches: query.includes('max-width'),
  }))
  await renderKeysPage({
    lane_policy: {
      mode: 'allow',
      allow_lanes: ['model-alpha', 'model-beta-with-a-long-name'],
      deny_lanes: [],
    },
    ip_allowlist: ['192.0.2.1', '2001:db8::1'],
  })
  await userEvent.click(screen.getByRole('button', { name: /Models: 2 model/ }))
  let details = await screen.findByRole('dialog')
  expect(within(details).getByText('model-alpha')).toBeVisible()
  expect(within(details).getByText('model-beta-with-a-long-name')).toBeVisible()
  await userEvent.keyboard('{Escape}')
  await userEvent.click(
    screen.getByRole('button', { name: /IP Restriction: 2 IP/ })
  )
  details = await screen.findByRole('dialog')
  expect(within(details).getByText('192.0.2.1')).toBeVisible()
  expect(within(details).getByText('2001:db8::1')).toBeVisible()
})

it('shows and copies the stored plaintext without rotating', async () => {
  const user = userEvent.setup()
  const { post } = await renderKeysPage()
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()

  const trigger = screen.getByRole('button', { name: 'pbr-existing-secret' })
  expect(trigger).toBeInTheDocument()
  expect(screen.queryByText(/sk-pbr-/)).not.toBeInTheDocument()

  await user.click(trigger)
  expect(await screen.findByText('Full API Key')).toBeVisible()
  expect(post).not.toHaveBeenCalled()

  await user.click(screen.getByRole('button', { name: 'Copy API key' }))
  await waitFor(() => expect(copy).toHaveBeenCalledWith('pbr-existing-secret'))
  expect(post).not.toHaveBeenCalled()
})

it('rotates only after an explicit confirmation and reveals the new key', async () => {
  const user = userEvent.setup()
  const { post } = await renderKeysPage({ key: null })
  post.mockResolvedValue({ data: { key: 'pbr-rotated-secret' } })

  await user.click(screen.getByRole('button', { name: 'pbr-abcd1234' }))
  await user.click(await screen.findByRole('button', { name: 'Rotate key' }))
  expect(post).not.toHaveBeenCalled()

  const dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText('Rotate this key?')).toBeVisible()
  await user.click(within(dialog).getByRole('button', { name: 'Rotate' }))

  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/keys/production/rotate', {})
  )
  expect(await screen.findByDisplayValue('pbr-rotated-secret')).toBeVisible()
})

it('distinguishes allow-all from allow-all-but-deny', async () => {
  await renderKeysPage({
    lane_policy: { mode: 'all', allow_lanes: [], deny_lanes: ['model-x'] },
  })
  expect(screen.getByText('All except 1')).toBeInTheDocument()
})
