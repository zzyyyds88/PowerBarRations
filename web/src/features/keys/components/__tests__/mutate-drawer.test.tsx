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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { apiKeySchema } from '../../types'
import { ApiKeysMutateDrawer } from '../api-keys-mutate-drawer'
import { ApiKeysProvider } from '../api-keys-provider'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

const pbrKey = {
  id: 7,
  name: 'production',
  enabled: true,
  key_prefix: 'pbr-abcd1234',
  lane_policy: { mode: 'allow', allow_lanes: ['model-alpha'], deny_lanes: [] },
  ip_allowlist: ['192.0.2.1'],
  expires_at: null,
  created_at: '2026-09-15T16:47:00Z',
  updated_at: '2026-09-15T16:47:00Z',
  last_used_at: null,
  cost: 0,
}

function renderDrawer() {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/keys') {
      return { data: { items: [pbrKey], next_cursor: null } }
    }
    if (url.startsWith('/api/keys/')) return { data: pbrKey }
    if (url === '/api/models') {
      return { data: { success: true, data: ['model-alpha', 'model-beta'] } }
    }
    return { data: { success: true, data: {} } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], {})
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <ApiKeysProvider>
          <ApiKeysMutateDrawer
            open
            onOpenChange={() => {}}
            currentRow={apiKeySchema.parse({
              id: 7,
              name: 'production',
              key: 'pbr-abcd1234',
              status: 1,
              cost: 0,
              expired_time: -1,
              created_time: 0,
              accessed_time: 0,
              model_limits_enabled: true,
              model_limits: 'model-alpha',
              allow_ips: '192.0.2.1',
            })}
          />
        </ApiKeysProvider>
      </QueryClientProvider>
    </I18nextProvider>
  )
  return client
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it('has no quota, wallet or subscription fields in the edit form', async () => {
  const client = renderDrawer()
  await waitFor(() =>
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue(
      'production'
    )
  )
  expect(screen.queryByLabelText(/Quota/i)).not.toBeInTheDocument()
  expect(screen.queryByLabelText(/Unlimited Quota/i)).not.toBeInTheDocument()
  expect(screen.queryByText(/Quota Settings/i)).not.toBeInTheDocument()
  for (const pattern of [/quota/i, /wallet/i, /subscription/i, /remaining/i]) {
    expect(screen.queryByText(pattern)).not.toBeInTheDocument()
  }
  client.clear()
})

it('saves the edit without sending any quota field', async () => {
  const put = vi.spyOn(api, 'put').mockResolvedValue({ data: pbrKey })
  const client = renderDrawer()
  const user = userEvent.setup()
  await waitFor(() =>
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue(
      'production'
    )
  )
  await user.click(screen.getByRole('button', { name: 'Save changes' }))
  await waitFor(() => expect(put).toHaveBeenCalled())
  const payload = put.mock.calls[0][1] as Record<string, unknown>
  expect(payload).not.toHaveProperty('remain_quota')
  expect(payload).not.toHaveProperty('used_quota')
  expect(payload).not.toHaveProperty('unlimited_quota')
  expect(payload).not.toHaveProperty('remain_quota_dollars')
  client.clear()
})
