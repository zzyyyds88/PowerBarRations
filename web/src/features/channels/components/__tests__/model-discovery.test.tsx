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
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError } from 'axios'
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
      return { data: editingChannel }
    }
    if (url === '/api/channel/default_base_urls') {
      return { data: {} }
    }
    if (url === '/api/prefill_group') {
      return { data: [] }
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
  const post = vi.spyOn(api, 'post').mockResolvedValue({ data: ['gpt-4'] })
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
  const post = vi.spyOn(api, 'post').mockResolvedValue({ data: ['gpt-4'] })
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

test('a successful manual probe opens the centered dialog and merges through Apply without replacing manual entries', async () => {
  mockChannelGet((url) => {
    if (url === '/api/channel/fetch_models/42') {
      return { data: ['manual-model', 'upstream-new'] }
    }
    return undefined
  })
  const user = userEvent.setup()
  render(<DiscoveryHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  await user.click(
    await screen.findByRole('button', { name: /Probe upstream models/ })
  )
  const dialog = await screen.findByRole('dialog', {
    name: 'Select upstream models',
  })
  expect(
    within(dialog).getByText(/Found 2 upstream models · 1 new · 1 existing/)
  ).toBeVisible()
  const apply = within(dialog).getByRole('button', { name: 'Apply' })
  expect(apply).toBeVisible()

  // Cancelling the picker must not touch the selected list.
  await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  await waitFor(() =>
    expect(
      screen.queryByRole('dialog', { name: 'Select upstream models' })
    ).not.toBeInTheDocument()
  )
  expect(
    within(screen.getByRole('group', { name: 'Models' })).queryByText(
      'upstream-new'
    )
  ).not.toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Re-fetch' }))
  const reopened = await screen.findByRole('dialog', {
    name: 'Select upstream models',
  })
  await user.click(within(reopened).getByRole('button', { name: 'Add all' }))
  await user.click(within(reopened).getByRole('button', { name: 'Apply' }))

  const models = screen.getByRole('group', { name: 'Models' })
  expect(within(models).getByText('manual-model')).toBeVisible()
  expect(within(models).getByText('upstream-new')).toBeVisible()
})

test('a failed manual probe offers an inline retry and the next success opens the dialog', async () => {
  let attempts = 0
  const rejection = new AxiosError('Upstream rejected the key')
  rejection.response = {
    data: {
      error: { code: 'upstream_error', message: 'Upstream rejected the key' },
    },
    status: 502,
    statusText: 'Bad Gateway',
    headers: {},
    config: { headers: {} },
  } as typeof rejection.response
  mockChannelGet((url) => {
    if (url !== '/api/channel/fetch_models/42') return undefined
    attempts += 1
    if (attempts === 1) {
      throw rejection
    }
    return { data: ['upstream-new'] }
  })
  const user = userEvent.setup()
  render(<DiscoveryHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  await user.click(
    await screen.findByRole('button', { name: /Probe upstream models/ })
  )
  expect(await screen.findByText('Upstream rejected the key')).toBeVisible()
  expect(
    screen.queryByRole('dialog', { name: 'Select upstream models' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Select upstream models',
  })
  expect(
    within(dialog).getByRole('checkbox', { name: 'upstream-new' })
  ).toBeVisible()
})

// ui-spec §6.4：模型区只有一个探测入口（「探测上游模型」）。已保存渠道必须复用
// 服务端已存密钥（GET /api/channel/fetch_models/{id}），不得发只有 base_url、没有 key
// 的草稿探测——表单出于安全不回填 key，那样必然 401。
test('the only discovery entry probes a saved channel through the stored key, never a keyless draft', async () => {
  mockChannelGet()
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/42') return { data: editingChannel }
    if (url === '/api/channel/default_base_urls') return { data: {} }
    if (url === '/api/prefill_group') return { data: [] }
    if (url === '/api/channel/fetch_models/42') {
      return { data: ['upstream-new'] }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
  const post = vi.spyOn(api, 'post')
  const user = userEvent.setup()
  render(<DiscoveryHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')

  // 不再有「获取模型列表」重复入口。
  expect(
    screen.queryByRole('button', { name: 'Fetch model list' })
  ).not.toBeInTheDocument()

  await user.click(
    await screen.findByRole('button', { name: /Probe upstream models/ })
  )

  // 探测走已保存渠道的密钥，且没有任何 keyless 草稿探测。
  expect(get).toHaveBeenCalledWith(
    '/api/channel/fetch_models/42',
    expect.anything()
  )
  expect(post).not.toHaveBeenCalledWith(
    '/api/channels/batch/fetch-models',
    expect.anything(),
    expect.anything()
  )

  const dialog = await screen.findByRole('dialog', {
    name: 'Select upstream models',
  })
  await user.click(
    within(dialog).getByRole('checkbox', { name: 'upstream-new' })
  )
  await user.click(within(dialog).getByRole('button', { name: 'Apply' }))

  const models = screen.getByRole('group', { name: 'Models' })
  expect(within(models).getByText('upstream-new')).toBeVisible()
})
