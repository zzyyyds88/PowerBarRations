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
  act,
  cleanup,
  render,
  renderHook,
  screen,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  useChannelModelDiscovery,
  type ChannelModelDiscoveryRequest,
} from '../../hooks/use-channel-model-discovery'
import { channelSchema, type Channel } from '../../types'
import { ChannelsProvider } from '../channels-provider'
import { ChannelMutateDialog } from '../drawers/channel-mutate-dialog'

const originalAuth = useAuthStore.getState().auth
let client: QueryClient
let editingChannel: Channel

function DiscoveryHarness(props: { currentRow?: Channel }) {
  const [open, setOpen] = useState(true)
  return (
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <ChannelMutateDialog
          open={open}
          onOpenChange={setOpen}
          currentRow={props.currentRow ?? null}
        />
      </ChannelsProvider>
    </QueryClientProvider>
  )
}

type GetOverride = (url: string) => { data: unknown } | undefined

function mockChannelGet(override?: GetOverride) {
  return vi.spyOn(api, 'get').mockImplementation(async (url) => {
    const replacement = override?.(url)
    if (replacement) return replacement
    if (url === '/api/channel/42') {
      return { data: { success: true, data: editingChannel } }
    }
    if (url === '/api/channel/default_base_urls') {
      return { data: { success: true, data: {} } }
    }
    if (url === '/api/group/') {
      return { data: { success: true, data: ['default'] } }
    }
    if (url === '/api/channel/models') {
      return { data: { success: true, data: [{ id: 'manual-model' }] } }
    }
    if (url === '/api/prefill_group') {
      return { data: { success: true, data: [] } }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
}

const previewRequest: ChannelModelDiscoveryRequest = {
  kind: 'preview',
  data: { type: 1, base_url: 'https://example.com', key: 'first-key' },
}

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  editingChannel = channelSchema.parse({
    id: 42,
    name: 'Existing channel',
    type: 1,
    key: '',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    models: 'manual-model',
    group: 'default',
    base_url: 'https://saved.example',
  })
  useAuthStore.setState({
    auth: {
      ...originalAuth,
      user: { id: 1, username: 'root', role: ROLE.SUPER_ADMIN },
    },
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.setState({ auth: originalAuth })
  vi.restoreAllMocks()
  vi.useRealTimers()
})

test('auto discovery fetches once after the 800ms debounce', async () => {
  vi.useFakeTimers()
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: ['gpt-4'] } })
  renderHook(() =>
    useChannelModelDiscovery({
      enabled: true,
      autoFetch: true,
      request: previewRequest,
    })
  )
  expect(post).not.toHaveBeenCalled()
  await act(async () => {
    vi.advanceTimersByTime(799)
  })
  expect(post).not.toHaveBeenCalled()
  await act(async () => {
    vi.advanceTimersByTime(1)
  })
  expect(post).toHaveBeenCalledTimes(1)
  await act(async () => {
    vi.advanceTimersByTime(5000)
  })
  expect(post).toHaveBeenCalledTimes(1)
})

test('a connection edit inside the debounce window collapses into one request', async () => {
  vi.useFakeTimers()
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: ['gpt-4'] } })
  const secondRequest: ChannelModelDiscoveryRequest = {
    kind: 'preview',
    data: { type: 1, base_url: 'https://example.com', key: 'second-key' },
  }
  const { rerender } = renderHook(
    (props: { request: ChannelModelDiscoveryRequest }) =>
      useChannelModelDiscovery({
        enabled: true,
        autoFetch: true,
        request: props.request,
      }),
    { initialProps: { request: previewRequest } }
  )
  await act(async () => {
    vi.advanceTimersByTime(500)
  })
  expect(post).not.toHaveBeenCalled()
  rerender({ request: secondRequest })
  await act(async () => {
    vi.advanceTimersByTime(500)
  })
  expect(post).not.toHaveBeenCalled()
  await act(async () => {
    vi.advanceTimersByTime(300)
  })
  expect(post).toHaveBeenCalledTimes(1)
  await act(async () => {
    vi.advanceTimersByTime(3000)
  })
  expect(post).toHaveBeenCalledTimes(1)
})

test('auto discovery stays idle while the connection is not ready', async () => {
  vi.useFakeTimers()
  const post = vi.spyOn(api, 'post')
  renderHook(() =>
    useChannelModelDiscovery({
      enabled: true,
      autoFetch: false,
      request: previewRequest,
    })
  )
  await act(async () => {
    vi.advanceTimersByTime(5000)
  })
  expect(post).not.toHaveBeenCalled()
})

test('a successful auto discovery merges into the model list without replacing manual entries', async () => {
  mockChannelGet((url) => {
    if (url === '/api/channel/fetch_models/42') {
      return { data: { success: true, data: ['manual-model', 'upstream-new'] } }
    }
    return undefined
  })
  const user = userEvent.setup()
  render(<DiscoveryHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  expect(
    await screen.findByText(/Found 2 upstream models · 1 new · 1 existing/)
  ).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Add all' }))

  const models = screen.getByRole('group', { name: 'Models' })
  expect(
    within(models).getByRole('button', { name: 'manual-model' })
  ).toBeVisible()
  expect(
    within(models).getByRole('button', { name: 'upstream-new' })
  ).toBeVisible()
})

test('a failed auto discovery offers an inline retry and recovers on success', async () => {
  let attempts = 0
  mockChannelGet((url) => {
    if (url !== '/api/channel/fetch_models/42') return undefined
    attempts += 1
    if (attempts === 1) {
      return { data: { success: false, message: 'Upstream rejected the key' } }
    }
    return { data: { success: true, data: ['upstream-new'] } }
  })
  const user = userEvent.setup()
  render(<DiscoveryHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  expect(await screen.findByText('Upstream rejected the key')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  expect(
    await screen.findByRole('checkbox', { name: 'upstream-new' })
  ).toBeVisible()
})
