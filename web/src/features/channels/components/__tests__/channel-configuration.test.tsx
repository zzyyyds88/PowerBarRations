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
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { createAppQueryClient } from '@/lib/query-client'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { channelSchema, type Channel } from '../../types'
import { ChannelsProvider } from '../channels-provider'
import { ChannelMutateDialog } from '../drawers/channel-mutate-dialog'

const originalAuth = useAuthStore.getState().auth
let client: QueryClient
let editingChannel: Channel

function deferredResponse<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((finish) => {
    resolve = finish
  })
  return { promise, resolve }
}

type UserEventInstance = ReturnType<typeof userEvent.setup>

// Opens the Basic Information type combobox and picks one of its options
// (built-in types render as "type:<n>").
async function selectTypeOption(
  user: UserEventInstance,
  name: string | RegExp
) {
  await user.click(screen.getByRole('combobox', { name: 'Type' }))
  await user.click(await screen.findByRole('option', { name }))
}

function ConfigurationHarness(props: {
  initialOpen?: boolean
  currentRow?: Channel
  clearRowOnClose?: boolean
}) {
  const [open, setOpen] = useState(props.initialOpen ?? true)
  return (
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <button type='button' onClick={() => setOpen(true)}>
          Open channel
        </button>
        <ChannelMutateDialog
          open={open}
          onOpenChange={setOpen}
          currentRow={open || !props.clearRowOnClose ? props.currentRow : null}
        />
      </ChannelsProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
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
    models: 'custom-model',
    group: 'default',
    base_url: 'https://saved.example',
  })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  useAuthStore.setState({
    auth: {
      ...originalAuth,
      user: { id: 1, username: 'root', role: ROLE.SUPER_ADMIN },
    },
  })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/42') {
      return { data: { success: true, data: editingChannel } }
    }
    if (url === '/api/channel/fetch_models/42') {
      return { data: { success: true, data: ['upstream-model'] } }
    }
    if (url === '/api/channel/models') {
      return { data: { success: true, data: [{ id: 'custom-model' }] } }
    }
    if (url === '/api/channel/default_base_urls') {
      return {
        data: {
          success: true,
          data: {
            22: 'https://fastgpt.server.example/api/openapi',
            24: 'https://gemini.server.example',
            43: 'https://deepseek.server.example',
            45: 'https://volcengine.server.example',
          },
        },
      }
    }
    if (url === '/api/prefill_group') {
      return { data: { success: true, data: [] } }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.setState({ auth: originalAuth })
  vi.restoreAllMocks()
})

test('changing built-in providers updates server-provided URL placeholders without replacing the draft address', async () => {
  const user = userEvent.setup()
  render(<ConfigurationHarness />)
  await selectTypeOption(user, 'DeepSeek')
  const address = screen.getByRole('textbox', { name: 'Base URL' })
  await waitFor(() =>
    expect(address).toHaveAttribute(
      'placeholder',
      'https://deepseek.server.example'
    )
  )
  expect(address).toHaveValue('')
  await user.type(address, 'https://custom.example')

  await selectTypeOption(user, 'Gemini')
  const geminiAddress = screen.getByRole('textbox', { name: 'Base URL' })
  expect(geminiAddress).toHaveAttribute(
    'placeholder',
    'https://gemini.server.example'
  )
  expect(geminiAddress).toHaveValue('https://custom.example')
  await user.clear(geminiAddress)
  expect(geminiAddress).toHaveValue('')

  await selectTypeOption(user, 'New API')
  expect(screen.getByRole('textbox', { name: 'Base URL' })).toHaveAttribute(
    'placeholder',
    'Leave empty to use default'
  )
})

test.each([
  {
    type: 43,
    label: /^Base URL$/,
    url: 'https://deepseek.server.example',
    savedUrl: '',
  },
  {
    type: 22,
    label: /^Private Deployment URL$/,
    url: 'https://fastgpt.server.example/api/openapi',
    savedUrl: '',
  },
  {
    type: 45,
    label: /^API Base URL/,
    url: 'https://volcengine.server.example',
    savedUrl: 'https://custom.example',
  },
])(
  'editing type $type keeps the server URL placeholder out of the saved address',
  async ({ type, label, url, savedUrl }) => {
    editingChannel.type = type
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    render(<ConfigurationHarness currentRow={editingChannel} />)
    await screen.findByDisplayValue('Existing channel')
    if (type === 45) {
      const addressLabel = screen.getByText('API Base URL')
      for (let click = 0; click < 10; click++) {
        fireEvent.click(addressLabel)
      }
    }
    const address = screen.getByRole('textbox', { name: label })
    await waitFor(() => expect(address).toHaveAttribute('placeholder', url))
    expect(address).toHaveValue('https://saved.example')
    await user.clear(address)
    expect(address).toHaveValue('')
    if (savedUrl) await user.type(address, savedUrl)
    await user.click(screen.getByRole('button', { name: 'Update Channel' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0]?.[1]).toMatchObject({ id: 42, base_url: savedUrl })
  }
)

test('an unavailable default URL endpoint keeps the fallback placeholder and allows saving a custom address', async () => {
  const onInternalServerError = vi.fn()
  client = createAppQueryClient(onInternalServerError)
  const originalGet = vi.mocked(api.get).getMockImplementation()
  vi.mocked(api.get).mockImplementation(async (url, config) => {
    if (url === '/api/channel/default_base_urls') {
      throw Object.assign(new Error('Endpoint unavailable'), {
        response: { status: 500 },
      })
    }
    return originalGet?.(url, config)
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  const address = screen.getByRole('textbox', { name: 'Base URL' })
  expect(address).toHaveAttribute('placeholder', 'Leave empty to use default')
  expect(address).toHaveValue('https://saved.example')
  await user.clear(address)
  await user.type(address, 'https://custom.example')
  await user.click(screen.getByRole('button', { name: 'Update Channel' }))
  await waitFor(() => expect(put).toHaveBeenCalled())
  expect(put.mock.calls[0]?.[1]).toMatchObject({
    id: 42,
    base_url: 'https://custom.example',
  })
  expect(api.get).toHaveBeenCalledWith('/api/channel/default_base_urls')
  expect(onInternalServerError).not.toHaveBeenCalled()
})

test('model mapping help opens on click, stays open after pointer exit, and closes without dismissing the channel', async () => {
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  await user.click(screen.getByRole('tab', { name: /Routing & Mapping/ }))
  const trigger = screen.getByRole('button', {
    name: 'How model mapping works',
  })
  expect(trigger).toHaveAttribute('type', 'button')
  expect(trigger).toHaveClass('size-4')
  expect(trigger).toHaveAttribute('aria-expanded', 'false')

  await user.click(trigger)
  const help = await screen.findByRole('dialog', { name: 'Request flow' })
  expect(trigger).toHaveAttribute('aria-expanded', 'true')
  expect(within(help).getByText('client-model')).toBeVisible()
  expect(within(help).getByText('upstream-model')).toBeVisible()
  await user.unhover(trigger)
  expect(help).toBeVisible()

  await user.keyboard('{Escape}')
  await waitFor(() => expect(help).not.toBeInTheDocument())
  expect(trigger).toHaveFocus()
  expect(trigger).toHaveAttribute('aria-expanded', 'false')
  expect(screen.getByRole('dialog', { name: 'Edit Channel' })).toBeVisible()
})

test('model mapping help supports keyboard activation and wraps long model names within the viewport', async () => {
  editingChannel.model_mapping =
    '{"customer-production-reasoning-model-alias":"provider/region/deployment/production-reasoning-model-version"}'
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  await user.click(screen.getByRole('tab', { name: /Routing & Mapping/ }))
  const trigger = screen.getByRole('button', {
    name: 'How model mapping works',
  })
  trigger.focus()
  await user.keyboard('{Enter}')
  const help = await screen.findByRole('dialog', { name: 'Request flow' })
  expect(help).toHaveClass(
    'flex-col',
    'w-96',
    'max-w-[calc(100vw-2rem)]',
    'text-sm'
  )
  expect(
    within(help).getByText('customer-production-reasoning-model-alias')
  ).toHaveClass('wrap-anywhere')
  expect(
    within(help).getByText(
      'provider/region/deployment/production-reasoning-model-version'
    )
  ).toHaveClass('wrap-anywhere')

  await user.keyboard('{Escape}')
  await waitFor(() => expect(help).not.toBeInTheDocument())
  await user.keyboard(' ')
  expect(
    await screen.findByRole('dialog', { name: 'Request flow' })
  ).toBeVisible()
  await user.click(screen.getByRole('tab', { name: /Routing & Mapping/ }))
  await waitFor(() =>
    expect(
      screen.queryByRole('dialog', { name: 'Request flow' })
    ).not.toBeInTheDocument()
  )
  expect(trigger).toHaveAttribute('aria-expanded', 'false')
})

test.each(['Cancel', 'Escape'])(
  '%s closes the edited channel dialog and discards unsaved changes',
  async (action) => {
    const user = userEvent.setup()
    render(<ConfigurationHarness currentRow={editingChannel} />)
    await screen.findByDisplayValue('Existing channel')
    await user.click(screen.getByRole('tab', { name: /Routing & Mapping/ }))
    fireEvent.change(screen.getByLabelText('Test Model'), {
      target: { value: 'gpt-4o-mini' },
    })
    if (action === 'Escape') {
      await user.keyboard('{Escape}')
    } else {
      await user.click(screen.getByRole('button', { name: 'Cancel' }))
    }
    // There is no "return to configuration" middle layer: closing dismisses
    // the whole dialog.
    await waitFor(() =>
      expect(
        screen.queryByRole('dialog', { name: 'Edit Channel' })
      ).not.toBeInTheDocument()
    )
    await user.click(screen.getByRole('button', { name: 'Open channel' }))
    expect(await screen.findByDisplayValue('Existing channel')).toBeVisible()
    expect(screen.getByRole('combobox', { name: 'Type' })).toHaveValue('OpenAI')
    await user.click(screen.getByRole('tab', { name: /Routing & Mapping/ }))
    expect(screen.getByLabelText('Test Model')).toHaveValue('')
  }
)

test('configuration navigation retains its height when the form content overflows', async () => {
  render(<ConfigurationHarness />)
  const navigation = screen.getByRole('tablist', {
    name: 'Channel configuration',
  })
  expect(navigation.parentElement).toHaveClass('shrink-0')
  expect(screen.getByRole('dialog', { name: 'Create Channel' })).toHaveClass(
    'sm:max-w-5xl'
  )
})

test('an invalid setting in another category is revealed and focused on submission', async () => {
  const user = userEvent.setup()
  render(<ConfigurationHarness />)
  await selectTypeOption(user, 'DeepSeek')
  fireEvent.change(screen.getByLabelText('API Key *'), {
    target: { value: 'secret' },
  })
  await user.click(
    screen.getByRole('combobox', { name: 'Select models or add custom ones' })
  )
  await user.click(await screen.findByRole('option', { name: 'custom-model' }))
  await user.keyboard('{Escape}')
  await user.click(screen.getByRole('tab', { name: /Other Settings/ }))
  fireEvent.change(screen.getByLabelText('Proxy Address'), {
    target: { value: 'invalid-proxy' },
  })
  await user.click(screen.getByRole('tab', { name: /Connection & Models/ }))
  await user.click(screen.getByRole('button', { name: 'Create Channel' }))
  await waitFor(() =>
    expect(screen.getByLabelText('Proxy Address')).toHaveFocus()
  )
  expect(screen.getByRole('tab', { name: /Other Settings/ })).toHaveAttribute(
    'aria-selected',
    'true'
  )
  expect(screen.getByLabelText('Proxy Address')).toHaveAttribute(
    'aria-invalid',
    'true'
  )
})

test('a failed creation keeps its draft and prevents duplicate submission while pending', async () => {
  const reply = deferredResponse<{
    data: { success: boolean; message: string }
  }>()
  // Only the channel-create call stays pending; model discovery resolves so a
  // late debounced fetch cannot be counted as a duplicate submission.
  const post = vi
    .spyOn(api, 'post')
    .mockImplementation(async (url) =>
      url === '/api/channel'
        ? reply.promise
        : { data: { success: true, data: [] } }
    )
  const user = userEvent.setup()
  render(<ConfigurationHarness />)
  await selectTypeOption(user, 'DeepSeek')
  fireEvent.change(screen.getByLabelText('API Key *'), {
    target: { value: 'keep-secret' },
  })
  await user.click(
    screen.getByRole('combobox', { name: 'Select models or add custom ones' })
  )
  await user.click(await screen.findByRole('option', { name: 'custom-model' }))
  await user.keyboard('{Escape}')
  await user.click(screen.getByRole('button', { name: 'Create Channel' }))
  expect(screen.getByRole('button', { name: 'Create Channel' })).toBeDisabled()
  await user.click(screen.getByRole('button', { name: 'Create Channel' }))
  // Model discovery (debounced on connection change) may fire alongside the
  // submit; only the channel-create call must not be duplicated.
  expect(
    post.mock.calls.filter(([url]) => url === '/api/channel')
  ).toHaveLength(1)
  await act(async () => {
    reply.resolve({
      data: { success: false, message: 'Upstream configuration rejected' },
    })
    await reply.promise
  })
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Create Channel' })).toBeEnabled()
  )
  expect(screen.getByLabelText('API Key *')).toHaveValue('keep-secret')
  expect(screen.getByLabelText('Name *')).toHaveValue('DeepSeek')
})

test('model discovery discards a response for old credentials and retains manually selected models', async () => {
  const oldReply = deferredResponse<{
    data: { success: boolean; data: string[] }
  }>()
  vi.spyOn(api, 'post')
    .mockReturnValueOnce(oldReply.promise)
    .mockResolvedValueOnce({
      data: { success: true, data: ['current-upstream-model'] },
    })
  const user = userEvent.setup()
  render(<ConfigurationHarness />)
  // The form opens on the default OpenAI type, so only the key is needed.
  fireEvent.change(screen.getByLabelText('API Key *'), {
    target: { value: 'first-key' },
  })
  await user.type(
    screen.getByRole('combobox', { name: 'Select models or add custom ones' }),
    'custom-model,'
  )
  await user.keyboard('{Escape}')
  expect(await screen.findByText('Fetching models...')).toBeVisible()
  fireEvent.change(screen.getByLabelText('API Key *'), {
    target: { value: 'next-key' },
  })
  expect(
    screen.getByText(
      'Connection settings changed. Fetch models again to refresh the list.'
    )
  ).toBeVisible()
  await act(async () => {
    oldReply.resolve({ data: { success: true, data: ['old-upstream-model'] } })
    await oldReply.promise
  })
  expect(screen.queryByText('old-upstream-model')).not.toBeInTheDocument()
  await user.click(
    await screen.findByRole('checkbox', { name: 'current-upstream-model' })
  )
  expect(
    screen.getByRole('button', { name: 'current-upstream-model' })
  ).toBeVisible()
  expect(screen.getByRole('button', { name: 'custom-model' })).toBeVisible()
})

test('model discovery reports failures inline and allows an empty result to fall back to manual models', async () => {
  vi.spyOn(api, 'post')
    .mockResolvedValueOnce({
      data: { success: false, message: 'Upstream rejected the key' },
    })
    .mockResolvedValueOnce({ data: { success: true, data: [] } })
  const user = userEvent.setup()
  render(<ConfigurationHarness />)
  // The form opens on the default OpenAI type, so only the key is needed.
  fireEvent.change(screen.getByLabelText('API Key *'), {
    target: { value: 'test-key' },
  })
  expect(await screen.findByText('Upstream rejected the key')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  expect(
    await screen.findByText('No models returned by the upstream')
  ).toBeVisible()
  await user.type(
    screen.getByRole('combobox', { name: 'Select models or add custom ones' }),
    'custom-model,'
  )
  await user.keyboard('{Escape}')
  expect(screen.getByRole('button', { name: 'custom-model' })).toBeVisible()
})

test('editing opens the shared configuration and omits an unchanged key on update', async () => {
  const channel = channelSchema.parse({
    id: 42,
    name: 'Existing channel',
    type: 1,
    key: '',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    models: 'custom-model',
    group: 'default',
  })
  const originalGet = vi.mocked(api.get).getMockImplementation()
  vi.mocked(api.get).mockImplementation(async (url, config) => {
    if (url === '/api/channel/42') {
      return { data: { success: true, data: channel } }
    }
    return originalGet?.(url, config)
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={channel} />)
  expect(await screen.findByDisplayValue('Existing channel')).toBeVisible()
  expect(screen.getByRole('combobox', { name: 'Type' })).toHaveValue('OpenAI')
  expect(screen.getAllByRole('tab')).toHaveLength(4)
  expect(
    screen.getByRole('tab', { name: /Connection & Models/ })
  ).toHaveAccessibleName(/Ready/)
  fireEvent.change(screen.getByLabelText('Name *'), {
    target: { value: 'Renamed channel' },
  })
  await user.click(screen.getByRole('button', { name: 'Update Channel' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/channel/',
      expect.objectContaining({ id: 42, name: 'Renamed channel' }),
      expect.anything()
    )
  )
  expect(put.mock.calls[0]?.[1]).not.toHaveProperty('key')
})

test('editing legacy channels retains the full provider list and saves the original type', async () => {
  editingChannel = { ...editingChannel, type: 55 }
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  expect(screen.getByRole('combobox', { name: 'Type' })).toHaveValue('Sora')
  await user.click(screen.getByRole('combobox', { name: 'Type' }))
  expect(screen.getByRole('option', { name: 'DoubaoVideo' })).toBeVisible()
  // Re-selecting the saved built-in type keeps the channel values.
  await user.click(screen.getByRole('option', { name: 'Sora' }))
  expect(screen.getByRole('button', { name: 'custom-model' })).toBeVisible()
  expect(screen.getByDisplayValue('https://saved.example')).toBeVisible()
  fireEvent.change(screen.getByLabelText('Name *'), {
    target: { value: 'Renamed legacy channel' },
  })
  await user.click(screen.getByRole('button', { name: 'Update Channel' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/channel/',
      expect.objectContaining({
        id: 42,
        type: 55,
        name: 'Renamed legacy channel',
      }),
      expect.anything()
    )
  )
})

test('a failed detail request blocks updating until retry loads the saved channel', async () => {
  const originalGet = vi.mocked(api.get).getMockImplementation()
  let fail = true
  vi.mocked(api.get).mockImplementation(async (url, config) => {
    if (url === '/api/channel/42' && fail) {
      fail = false
      throw new Error('Channel details unavailable')
    }
    return originalGet?.(url, config)
  })
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await waitFor(() =>
    expect(screen.getByText('Failed to load channel')).toBeVisible()
  )
  expect(screen.getByRole('button', { name: 'Update Channel' })).toBeDisabled()
  // While the saved channel is missing, the form is replaced by the error
  // state, so no type editing is possible at all.
  expect(
    screen.queryByRole('combobox', { name: 'Type' })
  ).not.toBeInTheDocument()
  expect(screen.queryByLabelText('Name *')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByDisplayValue('Existing channel')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Update Channel' })).toBeEnabled()
})

test('restoring routing defaults clears the configured indicator for both the block and category', async () => {
  // 渠道 priority/weight 已删除：本用例改用路由策略块里仍然存在的 test_model 触发"已配置"。
  editingChannel = { ...editingChannel, test_model: 'gpt-4o-mini' }
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  const tab = screen.getByRole('tab', { name: /Routing & Mapping/ })
  expect(tab).toHaveAccessibleName(/Configured/)
  const status = within(tab).getByRole('img', { name: 'Configured' })
  expect(status).toBeVisible()
  expect(tab).not.toHaveTextContent('Configured')
  await user.hover(status)
  expect(await screen.findByText('Configured')).toBeVisible()
  await user.unhover(status)
  await user.click(tab)
  const block = screen.getByRole('group', { name: 'Routing Strategy' })
  expect(within(block).getByRole('img', { name: 'Configured' })).toBeVisible()
  expect(block).toHaveClass('border-primary/35')
  fireEvent.change(screen.getByLabelText('Test Model'), {
    target: { value: '' },
  })
  expect(tab).not.toHaveAccessibleName(/Configured/)
  expect(
    within(block).queryByRole('img', { name: 'Configured' })
  ).not.toBeInTheDocument()
  expect(block).not.toHaveClass('border-primary/35')
  fireEvent.change(screen.getByLabelText('Test Model'), {
    target: { value: 'gpt-4o-mini' },
  })
  expect(tab).toHaveAccessibleName(/Configured/)
  expect(within(block).getByRole('img', { name: 'Configured' })).toBeVisible()
})

test('request processing configuration marks Other Settings but not the prices tab', async () => {
  editingChannel = {
    ...editingChannel,
    setting: '{"thinking_to_content":true}',
    param_override: '{}',
    header_override: '{}',
  }
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  expect(
    screen.getByRole('tab', { name: /Other Settings/ })
  ).toHaveAccessibleName(/Configured/)
  expect(
    screen.getByRole('tab', { name: /Upstream unit prices/ })
  ).not.toHaveAccessibleName(/Configured/)
  await user.click(screen.getByRole('tab', { name: /Other Settings/ }))
  expect(
    within(screen.getByRole('group', { name: 'Override Rules' })).queryByRole(
      'img',
      { name: 'Configured' }
    )
  ).not.toBeInTheDocument()
  const processing = screen.getByRole('group', { name: 'Request processing' })
  expect(
    within(processing).getByRole('img', { name: 'Configured' })
  ).toBeVisible()
  await user.click(screen.getByRole('switch', { name: 'Thinking to Content' }))
  expect(
    within(processing).queryByRole('img', { name: 'Configured' })
  ).not.toBeInTheDocument()
})

test('configuration from fields unsupported by the selected provider stays unmarked', async () => {
  editingChannel = {
    ...editingChannel,
    type: 24,
    setting: '{"force_format":true}',
    settings: '{"allow_speed":true,"allow_service_tier":true}',
  }
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  expect(
    screen.getByRole('tab', { name: /Other Settings/ })
  ).not.toHaveAccessibleName(/Configured/)
  expect(
    screen.getByRole('tab', { name: /Upstream unit prices/ })
  ).not.toHaveAccessibleName(/Configured/)
})

test('an invalid edit switches categories and replaces configured styling with the field error', async () => {
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  await user.click(screen.getByRole('tab', { name: /Other Settings/ }))
  fireEvent.change(screen.getByLabelText('Proxy Address'), {
    target: { value: 'invalid-proxy' },
  })
  await user.click(screen.getByRole('tab', { name: /Connection & Models/ }))
  await user.click(screen.getByRole('button', { name: 'Update Channel' }))
  await waitFor(() =>
    expect(screen.getByLabelText('Proxy Address')).toHaveFocus()
  )
  expect(
    screen.getByRole('tab', { name: /Other Settings/ })
  ).toHaveAccessibleName(/Error/)
  const block = screen.getByRole('group', { name: 'Channel Extra Settings' })
  expect(within(block).getByRole('img', { name: 'Error' })).toBeVisible()
  expect(
    within(block).queryByRole('img', { name: 'Configured' })
  ).not.toBeInTheDocument()
  expect(block).toHaveClass('border-destructive/50')
  fireEvent.change(screen.getByLabelText('Proxy Address'), {
    target: { value: '' },
  })
  await waitFor(() =>
    expect(
      within(block).queryByRole('img', { name: 'Error' })
    ).not.toBeInTheDocument()
  )
  expect(
    within(block).queryByRole('img', { name: 'Configured' })
  ).not.toBeInTheDocument()
})

test('ordinary edits discover models with saved settings and keep removed draft models available for reselection', async () => {
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post')
  const put = vi.spyOn(api, 'put')
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  fireEvent.change(screen.getByDisplayValue('https://saved.example'), {
    target: { value: 'https://draft.example' },
  })
  fireEvent.change(screen.getByLabelText('API Key *'), {
    target: { value: 'new-key' },
  })
  await user.click(
    await screen.findByRole('checkbox', { name: 'upstream-model' })
  )
  expect(api.get).toHaveBeenCalledWith(
    '/api/channel/fetch_models/42',
    expect.anything()
  )
  expect(screen.getByRole('button', { name: 'custom-model' })).toBeVisible()
  expect(screen.getByRole('button', { name: 'upstream-model' })).toBeVisible()
  expect(screen.getByRole('tab', { name: 'New Models (1)' })).toBeVisible()
  expect(screen.getByRole('tab', { name: 'Removed Models (1)' })).toBeVisible()
  await user.type(
    screen.getByRole('combobox', { name: 'Select models or add custom ones' }),
    'manual-draft,'
  )
  await user.keyboard('{Escape}')
  const removedTab = screen.getByRole('tab', { name: 'Removed Models (2)' })
  await user.click(removedTab)
  for (const model of ['custom-model', 'manual-draft']) {
    await user.click(screen.getByRole('checkbox', { name: model }))
    expect(
      screen.queryByRole('button', { name: model })
    ).not.toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: model })).not.toBeChecked()
    expect(removedTab).toHaveAttribute('aria-selected', 'true')
  }
  await user.click(screen.getByRole('checkbox', { name: 'manual-draft' }))
  expect(screen.getByRole('button', { name: 'manual-draft' })).toBeVisible()
  expect(screen.getByRole('checkbox', { name: 'manual-draft' })).toBeChecked()
  expect(screen.getAllByRole('dialog')).toHaveLength(1)
  expect(post).not.toHaveBeenCalled()
  expect(put).not.toHaveBeenCalled()
})

test('model configuration replaces the selected-count badge at the right of the model field header', async () => {
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')

  const models = screen.getByRole('group', { name: 'Models' })
  const input = within(models).getByRole('combobox', {
    name: 'Select models or add custom ones',
  })
  const configure = within(models).getByRole('button', {
    name: 'Configure Models',
  })
  expect(configure.parentElement).toHaveClass(
    'flex',
    'items-start',
    'justify-between'
  )
  expect(configure).toHaveClass('shrink-0')
  expect(
    configure.compareDocumentPosition(input) & Node.DOCUMENT_POSITION_FOLLOWING
  ).toBeTruthy()
  expect(screen.queryByText('Selected 1')).not.toBeInTheDocument()
  expect(
    screen.getAllByRole('button', { name: 'Configure Models' })
  ).toHaveLength(1)
})

test('model configuration uses only the current form models and persists changes only when the channel is saved', async () => {
  editingChannel.models = 'gpt-one,manual-model,alias'
  const post = vi.spyOn(api, 'post')
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  await screen.findByRole('checkbox', { name: 'upstream-model' })
  await user.type(
    screen.getByRole('combobox', { name: 'Select models or add custom ones' }),
    'gpt-two,'
  )
  await user.keyboard('{Escape}')
  const models = screen.getByRole('group', { name: 'Models' })
  const manualModelChip = within(models).getByRole('button', {
    name: 'manual-model',
  })
  vi.mocked(api.get).mockClear()

  const trigger = screen.getByRole('button', { name: 'Configure Models' })
  await user.click(trigger)
  const dialog = within(
    screen.getByRole('dialog', { name: 'Configure Models' })
  )
  for (const model of ['gpt-one', 'manual-model', 'alias', 'gpt-two']) {
    expect(dialog.getByRole('checkbox', { name: model })).toBeChecked()
  }
  expect(
    dialog.queryByRole('checkbox', { name: 'upstream-model' })
  ).not.toBeInTheDocument()
  expect(
    dialog.queryByRole('checkbox', { name: 'custom-model' })
  ).not.toBeInTheDocument()
  expect(dialog.queryByRole('tab')).not.toBeInTheDocument()
  expect(dialog.getByText('Current models: 4')).toBeVisible()
  await user.click(dialog.getByRole('checkbox', { name: 'manual-model' }))
  expect(manualModelChip).toBeInTheDocument()
  expect(put).not.toHaveBeenCalled()

  await user.click(dialog.getByRole('button', { name: 'Apply' }))

  expect(
    within(models).queryByRole('button', { name: 'manual-model' })
  ).not.toBeInTheDocument()
  await waitFor(() => expect(trigger).toHaveFocus())
  expect(api.get).not.toHaveBeenCalled()
  expect(post).not.toHaveBeenCalled()
  expect(put).not.toHaveBeenCalled()
  await user.click(trigger)
  const reopened = within(
    screen.getByRole('dialog', { name: 'Configure Models' })
  )
  expect(
    reopened.queryByRole('checkbox', { name: 'manual-model' })
  ).not.toBeInTheDocument()
  expect(reopened.getByRole('checkbox', { name: 'gpt-two' })).toBeChecked()
  await user.click(reopened.getByRole('button', { name: 'Cancel' }))
  await user.click(screen.getByRole('button', { name: 'Update Channel' }))
  await waitFor(() => expect(put).toHaveBeenCalled())
  expect(put.mock.calls[0]?.[1]).toMatchObject({
    id: 42,
    models: 'gpt-one,alias,gpt-two',
  })
})

test.each(['Cancel', 'Close', 'Escape'])(
  'model configuration discards changes on %s and restores focus to its trigger',
  async (action) => {
    editingChannel.models = 'gpt-one,gpt-two'
    const user = userEvent.setup()
    render(<ConfigurationHarness currentRow={editingChannel} />)
    await screen.findByDisplayValue('Existing channel')
    const trigger = screen.getByRole('button', { name: 'Configure Models' })
    trigger.focus()
    await user.keyboard('{Enter}')
    const dialog = within(
      screen.getByRole('dialog', { name: 'Configure Models' })
    )
    const model = dialog.getByRole('checkbox', { name: 'gpt-one' })
    model.focus()
    await user.keyboard(' ')
    expect(model).not.toBeChecked()

    if (action === 'Escape') {
      await user.keyboard('{Escape}')
    } else {
      await user.click(dialog.getByRole('button', { name: action }))
    }

    await waitFor(() => expect(trigger).toHaveFocus())
    expect(screen.getByRole('button', { name: 'gpt-one' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'gpt-two' })).toBeVisible()
    await user.click(trigger)
    expect(
      within(
        screen.getByRole('dialog', { name: 'Configure Models' })
      ).getByRole('checkbox', { name: 'gpt-one' })
    ).toBeChecked()
  }
)

test('model configuration keeps unchecked candidates searchable and supports category selection and applying an empty list', async () => {
  editingChannel.models = 'gpt-one,gpt-two,manual-model'
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  const trigger = screen.getByRole('button', { name: 'Configure Models' })
  await user.click(trigger)
  const dialog = within(
    screen.getByRole('dialog', { name: 'Configure Models' })
  )
  const category = dialog.getByRole('checkbox', {
    name: 'Select all models in OpenAI',
  })
  await user.click(category)
  expect(dialog.getByRole('checkbox', { name: 'gpt-one' })).not.toBeChecked()
  expect(dialog.getByRole('checkbox', { name: 'gpt-two' })).not.toBeChecked()
  await user.click(dialog.getByRole('checkbox', { name: 'gpt-one' }))
  expect(category).toHaveAttribute('aria-checked', 'mixed')
  const search = dialog.getByRole('textbox', { name: 'Search models...' })
  await user.type(search, 'gpt-')
  await user.click(
    dialog.getByRole('button', { name: 'Select all matching models' })
  )
  expect(dialog.getByRole('checkbox', { name: 'gpt-two' })).toBeChecked()
  expect(
    dialog.queryByRole('checkbox', { name: 'manual-model' })
  ).not.toBeInTheDocument()
  await user.clear(search)
  await user.type(search, 'missing')
  expect(dialog.getByText('No matching items')).toBeVisible()
  expect(
    dialog.getByRole('button', { name: 'Select all matching models' })
  ).toBeDisabled()
  await user.clear(search)
  expect(dialog.getByRole('checkbox', { name: 'manual-model' })).toBeChecked()
  await user.click(
    dialog.getByRole('checkbox', { name: 'Select all models in OpenAI' })
  )
  await user.click(
    dialog.getByRole('checkbox', { name: 'Select all models in Other' })
  )
  await user.click(dialog.getByRole('button', { name: 'Apply' }))

  expect(
    screen.queryByRole('button', { name: 'manual-model' })
  ).not.toBeInTheDocument()
  expect(trigger).toBeDisabled()
})

test('advanced custom edits preview draft connection settings with the saved key', async () => {
  editingChannel = {
    ...editingChannel,
    type: 58,
    settings: JSON.stringify({
      advanced_custom: {
        advanced_routes: [
          {
            incoming_path: '/v1/models',
            upstream_path: '/v1/models',
            converter: 'none',
          },
        ],
      },
    }),
  }
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: ['preview-model'] } })
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  fireEvent.change(screen.getByDisplayValue('https://saved.example'), {
    target: { value: 'https://draft.example' },
  })
  fireEvent.change(screen.getByLabelText('API Key *'), {
    target: { value: 'unsaved-key' },
  })
  expect(
    await screen.findByRole('checkbox', { name: 'preview-model' })
  ).toBeVisible()
  expect(post).toHaveBeenCalledWith(
    '/api/channel/fetch_models',
    expect.objectContaining({
      type: 58,
      channel_id: 42,
      base_url: 'https://draft.example',
      key: undefined,
    }),
    expect.anything()
  )
  expect(api.get).not.toHaveBeenCalledWith(
    '/api/channel/fetch_models/42',
    expect.anything()
  )
})

test('an operator without sensitive write permission can discover saved models and update routing', async () => {
  useAuthStore.setState({
    auth: {
      ...originalAuth,
      user: {
        id: 10,
        username: 'operator',
        role: ROLE.ADMIN,
        permissions: {
          admin_permissions: {
            channel: { read: true, write: true, operate: true },
          },
        },
      },
    },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  expect(screen.getByRole('combobox', { name: 'Type' })).toBeDisabled()
  expect(screen.getByLabelText('API Key *')).toBeDisabled()
  expect(
    await screen.findByRole('checkbox', { name: 'upstream-model' })
  ).toBeVisible()
  await user.click(screen.getByRole('tab', { name: /Other Settings/ }))
  const thinking = screen.getByRole('switch', { name: 'Thinking to Content' })
  expect(thinking).toHaveAttribute('aria-disabled', 'true')
  await user.click(thinking)
  expect(thinking).not.toBeChecked()
  await user.click(screen.getByRole('tab', { name: /Other Settings/ }))
  expect(screen.getByLabelText('Proxy Address')).toBeDisabled()
  await user.click(screen.getByRole('tab', { name: /Routing & Mapping/ }))
  fireEvent.change(screen.getByLabelText('Test Model'), {
    target: { value: 'gpt-4o-mini' },
  })
  await user.click(screen.getByRole('button', { name: 'Update Channel' }))
  await waitFor(() => expect(put).toHaveBeenCalled())
  expect(put.mock.calls[0]?.[1]).toMatchObject({
    id: 42,
    test_model: 'gpt-4o-mini',
  })
  expect(put.mock.calls[0]?.[1]).not.toHaveProperty('setting')
  expect(put.mock.calls[0]?.[1]).not.toHaveProperty('key')
})

test.each([
  ['random', 'Random', 'polling', 'Polling'],
  ['polling', 'Polling', 'random', 'Random'],
] as const)(
  'multi-key editing switches from %s without replacing keys',
  async (initialMode, initialLabel, nextMode, nextLabel) => {
    editingChannel.channel_info = {
      ...editingChannel.channel_info,
      is_multi_key: true,
      multi_key_size: 2,
      multi_key_mode: initialMode,
    }
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    render(<ConfigurationHarness currentRow={editingChannel} />)
    await screen.findByDisplayValue('Existing channel')
    const strategy = screen.getByRole('combobox', {
      name: 'Multi-Key Strategy',
    })
    expect(strategy).toHaveTextContent(initialLabel)
    await user.click(strategy)
    await user.click(screen.getByRole('option', { name: nextLabel }))
    expect(strategy).toHaveTextContent(nextLabel)
    await user.click(screen.getByRole('button', { name: 'Update Channel' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0]?.[1]).toMatchObject({
      id: 42,
      multi_key_mode: nextMode,
    })
    expect(put.mock.calls[0]?.[1]).not.toHaveProperty('key')
    expect(put.mock.calls[0]?.[1]).not.toHaveProperty('key_mode')
  }
)

test('single-key editing omits the multi-key strategy control and update field', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  expect(
    screen.queryByRole('combobox', { name: 'Multi-Key Strategy' })
  ).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Update Channel' }))
  await waitFor(() => expect(put).toHaveBeenCalled())
  expect(put.mock.calls[0]?.[1]).not.toHaveProperty('multi_key_mode')
})

test.each(['append', 'replace'])(
  'multi-key editing submits the selected %s mode with new keys',
  async (mode) => {
    editingChannel = {
      ...editingChannel,
      channel_info: {
        ...editingChannel.channel_info,
        is_multi_key: true,
        multi_key_size: 2,
      },
    }
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    render(<ConfigurationHarness currentRow={editingChannel} />)
    await screen.findByDisplayValue('Existing channel')
    if (mode === 'replace') {
      await user.click(
        screen.getByRole('combobox', { name: 'Key Update Mode' })
      )
      await user.click(
        screen.getByRole('option', { name: 'Replace all existing keys' })
      )
    }
    fireEvent.change(screen.getByLabelText('API Key *'), {
      target: { value: 'next-key' },
    })
    await user.click(screen.getByRole('button', { name: 'Update Channel' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0]?.[1]).toMatchObject({
      key: 'next-key',
      key_mode: mode,
    })
  }
)

test('a failed update retains the draft through a background detail refetch', async () => {
  vi.spyOn(api, 'put').mockResolvedValue({
    data: { success: false, message: 'Update failed' },
  })
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  fireEvent.change(screen.getByLabelText('Name *'), {
    target: { value: 'Unsaved name' },
  })
  await user.click(screen.getByRole('button', { name: 'Update Channel' }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Update Channel' })).toBeEnabled()
  )
  await act(async () => {
    await client.refetchQueries({ queryKey: ['channels'] })
  })
  expect(screen.getByDisplayValue('Unsaved name')).toBeVisible()
})

test('switching edited channels discards a pending model list from the previous channel', async () => {
  const reply = deferredResponse<{
    data: { success: boolean; data: string[] }
  }>()
  const otherChannel = { ...editingChannel, id: 43, name: 'Second channel' }
  const originalGet = vi.mocked(api.get).getMockImplementation()
  vi.mocked(api.get).mockImplementation(async (url, config) => {
    if (url === '/api/channel/fetch_models/42') return reply.promise
    if (url === '/api/channel/43') {
      return { data: { success: true, data: otherChannel } }
    }
    if (url === '/api/channel/fetch_models/43') {
      return { data: { success: true, data: ['second-model'] } }
    }
    return originalGet?.(url, config)
  })
  const view = render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  expect(await screen.findByText('Fetching models...')).toBeVisible()
  view.rerender(<ConfigurationHarness currentRow={otherChannel} />)
  await screen.findByDisplayValue('Second channel')
  expect(
    await screen.findByRole('checkbox', { name: 'second-model' })
  ).toBeVisible()
  await act(async () => {
    reply.resolve({ data: { success: true, data: ['first-model'] } })
  })
  expect(
    screen.queryByRole('checkbox', { name: 'first-model' })
  ).not.toBeInTheDocument()
  expect(screen.getByRole('checkbox', { name: 'second-model' })).toBeVisible()
})

test('an unknown saved type remains editable without selecting a new provider', async () => {
  editingChannel = { ...editingChannel, type: 999 }
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  render(<ConfigurationHarness currentRow={editingChannel} />)
  expect(await screen.findByDisplayValue('Existing channel')).toBeVisible()
  // Unknown saved types stay editable and display the raw type number.
  expect(screen.getByRole('combobox', { name: 'Type' })).toHaveValue('type:999')
  await userEvent.click(screen.getByRole('button', { name: 'Update Channel' }))
  await waitFor(() => expect(put).toHaveBeenCalled())
  expect(put.mock.calls[0]?.[1]).toMatchObject({ id: 42, type: 999 })
})

test('a background refresh updates untouched values without moving the selected category', async () => {
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  await user.click(screen.getByRole('tab', { name: /Routing & Mapping/ }))
  editingChannel = { ...editingChannel, test_model: 'gpt-4o-mini' }
  await act(async () => {
    await client.refetchQueries({ queryKey: ['channels'] })
  })
  expect(
    screen.getByRole('tab', { name: /Routing & Mapping/ })
  ).toHaveAttribute('aria-selected', 'true')
  await waitFor(() =>
    expect(screen.getByLabelText('Test Model')).toHaveValue('gpt-4o-mini')
  )
})

test('closing an edited channel clears the row so the next open starts a fresh creation', async () => {
  const user = userEvent.setup()
  const view = render(
    <ConfigurationHarness currentRow={editingChannel} clearRowOnClose />
  )
  await screen.findByDisplayValue('Existing channel')
  expect(screen.getByRole('dialog', { name: 'Edit Channel' })).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Cancel' }))
  await waitFor(() =>
    expect(
      screen.queryByRole('dialog', { name: 'Edit Channel' })
    ).not.toBeInTheDocument()
  )
  view.rerender(<ConfigurationHarness clearRowOnClose />)
  await user.click(screen.getByRole('button', { name: 'Open channel' }))
  expect(await screen.findByRole('dialog', { name: 'Create Channel' })).toBeVisible()
  expect(screen.getByRole('combobox', { name: 'Type' })).toHaveValue('OpenAI')
  expect(screen.getByRole('textbox', { name: /^Name\s*\*$/ })).toHaveValue('')
})

// 上游单价编辑器必须与模型清单同屏（design-v1 §16#7）：每个模型在不同渠道的采购价
// 不同，埋进「其他设置 → 渠道额外设置」没人找得到。默认分区就是「连接与模型」，
// 因此不切换任何 tab 也能看到它，即可证明落点正确。
test('upstream unit price editor sits with the model list in the default section', async () => {
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')

  expect(
    screen.getByRole('tab', { name: /Connection & Models/ })
  ).toHaveAttribute('aria-selected', 'true')
  expect(screen.getByText('Upstream unit prices')).toBeVisible()
})
