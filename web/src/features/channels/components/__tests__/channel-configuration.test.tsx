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

// The Models area is now a plain "selected list + single-value add" (ui-spec
// §6.4). These helpers keep each test focused on behavior rather than layout.
function modelsGroup() {
  return within(screen.getByRole('group', { name: 'Models' }))
}

async function addManualModel(user: UserEventInstance, model: string) {
  await user.type(modelsGroup().getByLabelText('Add a model manually'), model)
  await user.click(modelsGroup().getByRole('button', { name: 'Add' }))
}

async function openDiscoveryDialog() {
  return within(
    await screen.findByRole('dialog', { name: 'Select upstream models' })
  )
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
  // ui-spec §6.9：外框尺寸只由 lg 档决定，正文自身滚动。
  const dialog = screen.getByRole('dialog', { name: 'Create Channel' })
  expect(dialog.className).toContain('w-[min(94vw,960px)]')
  expect(dialog.className).toContain('h-[min(82vh,640px)]')
})

test('an invalid setting in another category is revealed and focused on submission', async () => {
  const user = userEvent.setup()
  render(<ConfigurationHarness />)
  await selectTypeOption(user, 'DeepSeek')
  fireEvent.change(screen.getByLabelText('API Key *'), {
    target: { value: 'secret' },
  })
  const models = screen.getByRole('group', { name: 'Models' })
  await user.type(
    within(models).getByLabelText('Add a model manually'),
    'custom-model'
  )
  await user.click(within(models).getByRole('button', { name: 'Add' }))
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
  const models = screen.getByRole('group', { name: 'Models' })
  await user.type(
    within(models).getByLabelText('Add a model manually'),
    'custom-model'
  )
  await user.click(within(models).getByRole('button', { name: 'Add' }))
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
  await addManualModel(user, 'custom-model')
  await user.click(
    await screen.findByRole('button', { name: /Probe upstream models/ })
  )
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
  // The stale response must not open the dialog nor leak its candidates.
  expect(
    screen.queryByRole('dialog', { name: 'Select upstream models' })
  ).not.toBeInTheDocument()
  expect(screen.queryByText('old-upstream-model')).not.toBeInTheDocument()
  await user.click(
    await screen.findByRole('button', { name: /Re-fetch|Probe upstream models/ })
  )
  const dialog = await openDiscoveryDialog()
  await user.click(
    dialog.getByRole('checkbox', { name: 'current-upstream-model' })
  )
  await user.click(dialog.getByRole('button', { name: 'Apply' }))
  expect(modelsGroup().getByText('current-upstream-model')).toBeVisible()
  expect(modelsGroup().getByText('custom-model')).toBeVisible()
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
  await user.click(
    await screen.findByRole('button', { name: /Probe upstream models/ })
  )
  expect(await screen.findByText('Upstream rejected the key')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  expect(
    await screen.findByText('No models returned by the upstream')
  ).toBeVisible()
  // An empty upstream result must not open an empty picker.
  expect(
    screen.queryByRole('dialog', { name: 'Select upstream models' })
  ).not.toBeInTheDocument()
  await addManualModel(user, 'custom-model')
  expect(modelsGroup().getByText('custom-model')).toBeVisible()
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
  expect(modelsGroup().getByText('custom-model')).toBeVisible()
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
    await screen.findByRole('button', { name: /Probe upstream models/ })
  )
  const dialog = await openDiscoveryDialog()
  expect(api.get).toHaveBeenCalledWith(
    '/api/channel/fetch_models/42',
    expect.anything()
  )
  // The saved model stays checked; the newly discovered candidate starts
  // unchecked and is added explicitly.
  expect(dialog.getByRole('checkbox', { name: 'custom-model' })).toBeChecked()
  const candidate = dialog.getByRole('checkbox', { name: 'upstream-model' })
  expect(candidate).not.toBeChecked()
  await user.click(candidate)
  await user.click(dialog.getByRole('button', { name: 'Apply' }))

  expect(modelsGroup().getByText('upstream-model')).toBeVisible()
  expect(modelsGroup().getByText('custom-model')).toBeVisible()

  // Draft-only manual model joins the form list and stays available.
  await addManualModel(user, 'manual-draft')
  expect(modelsGroup().getByText('manual-draft')).toBeVisible()

  // Applying and editing the draft must not hit the network.
  expect(post).not.toHaveBeenCalled()
  expect(put).not.toHaveBeenCalled()
})

test('the Models area keeps no always-on combobox and focuses the manual input without opening candidates', async () => {
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')

  const models = modelsGroup()
  // The base "all declared models" multi-select is gone (ui-spec §6.4): there
  // must be no combobox at all in the Models area.
  expect(models.queryByRole('combobox')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('combobox', { name: 'Select models or add custom ones' })
  ).not.toBeInTheDocument()

  const input = models.getByLabelText('Add a model manually')
  input.focus()
  expect(input).toHaveFocus()
  // Focusing/typing a partial name must not surface unrelated candidates.
  fireEvent.change(input, { target: { value: 'gpt' } })
  expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  expect(screen.queryByRole('option')).not.toBeInTheDocument()

  expect(models.getByText('custom-model')).toBeVisible()
})

test('probing opens a centered dialog that seeds the current selection and persists changes only when the channel is saved', async () => {
  editingChannel.models = 'gpt-one,manual-model,alias'
  const post = vi.spyOn(api, 'post')
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  // 'upstream-model' is returned on re-fetch in this suite's GET mock.
  await user.click(
    await screen.findByRole('button', { name: /Probe upstream models/ })
  )
  const dialog = await openDiscoveryDialog()
  // Candidates are checked against the saved list: the existing form models
  // start checked while the newly discovered candidate starts unchecked.
  for (const model of ['gpt-one', 'manual-model', 'alias']) {
    expect(dialog.getByRole('checkbox', { name: model })).toBeChecked()
  }
  expect(
    dialog.getByRole('checkbox', { name: 'upstream-model' })
  ).not.toBeChecked()

  await user.click(dialog.getByRole('checkbox', { name: 'upstream-model' }))
  await user.click(dialog.getByRole('checkbox', { name: 'manual-model' }))
  // Nothing is written to the channel until Apply.
  expect(put).not.toHaveBeenCalled()

  await user.click(dialog.getByRole('button', { name: 'Apply' }))

  const models = modelsGroup()
  expect(models.getByText('upstream-model')).toBeVisible()
  expect(models.queryByText('manual-model')).not.toBeInTheDocument()
  expect(post).not.toHaveBeenCalled()
  expect(put).not.toHaveBeenCalled()

  await user.click(screen.getByRole('button', { name: 'Update Channel' }))
  await waitFor(() => expect(put).toHaveBeenCalled())
  expect(put.mock.calls[0]?.[1]).toMatchObject({
    id: 42,
    models: 'gpt-one,alias,upstream-model',
  })
})

test.each(['Cancel', 'Escape'])(
  'dismissing the discovery dialog on %s leaves the selected models unchanged',
  async (action) => {
    editingChannel.models = 'gpt-one,gpt-two'
    const user = userEvent.setup()
    render(<ConfigurationHarness currentRow={editingChannel} />)
    await screen.findByDisplayValue('Existing channel')
    await user.click(
      await screen.findByRole('button', { name: /Probe upstream models/ })
    )
    const dialog = await openDiscoveryDialog()
    const candidate = dialog.getByRole('checkbox', { name: 'upstream-model' })
    candidate.focus()
    await user.keyboard(' ')
    expect(candidate).toBeChecked()

    if (action === 'Escape') {
      await user.keyboard('{Escape}')
    } else {
      await user.click(dialog.getByRole('button', { name: 'Cancel' }))
    }

    await waitFor(() =>
      expect(
        screen.queryByRole('dialog', { name: 'Select upstream models' })
      ).not.toBeInTheDocument()
    )
    const models = modelsGroup()
    expect(models.getByText('gpt-one')).toBeVisible()
    expect(models.getByText('gpt-two')).toBeVisible()
    expect(models.queryByText('upstream-model')).not.toBeInTheDocument()

    // Re-running the probe reopens with the (unchanged) current selection.
    await user.click(screen.getByRole('button', { name: 'Re-fetch' }))
    const reopened = await openDiscoveryDialog()
    expect(
      reopened.getByRole('checkbox', { name: 'upstream-model' })
    ).not.toBeChecked()
  }
)

test('the discovery dialog distinguishes existing from new candidates and applies the current selection', async () => {
  editingChannel.models = 'gpt-one,gpt-two,manual-model'
  const originalGet = vi.mocked(api.get).getMockImplementation()
  vi.mocked(api.get).mockImplementation(async (url, config) => {
    if (url === '/api/channel/fetch_models/42') {
      return {
        data: {
          success: true,
          data: ['gpt-one', 'gpt-two', 'fresh-alpha', 'fresh-beta'],
        },
      }
    }
    return originalGet?.(url, config)
  })
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  await user.click(
    await screen.findByRole('button', { name: /Probe upstream models/ })
  )
  const dialog = await openDiscoveryDialog()
  // Saved models the upstream still returns start checked; new ones do not.
  expect(dialog.getByRole('checkbox', { name: 'gpt-one' })).toBeChecked()
  expect(dialog.getByRole('checkbox', { name: 'gpt-two' })).toBeChecked()
  expect(dialog.getByRole('checkbox', { name: 'fresh-alpha' })).not.toBeChecked()
  expect(dialog.getByRole('checkbox', { name: 'fresh-beta' })).not.toBeChecked()

  // A draft-only model that upstream no longer returns stays selected unless
  // the user unchecks it.
  await user.click(dialog.getByRole('button', { name: 'Add new only' }))
  await user.click(dialog.getByRole('button', { name: 'Apply' }))

  const models = modelsGroup()
  for (const model of [
    'gpt-one',
    'gpt-two',
    'manual-model',
    'fresh-alpha',
    'fresh-beta',
  ]) {
    expect(models.getByText(model)).toBeVisible()
  }
})

test('selected models can be removed and manual additions are trimmed and de-duplicated', async () => {
  editingChannel.models = 'gpt-one,gpt-two'
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  const models = modelsGroup()

  await user.click(models.getByRole('button', { name: 'Remove gpt-one' }))
  expect(models.queryByText('gpt-one')).not.toBeInTheDocument()
  expect(models.getByText('gpt-two')).toBeVisible()

  // Duplicate of an already selected model is ignored, not listed twice.
  await user.type(models.getByLabelText('Add a model manually'), 'gpt-two')
  await user.click(models.getByRole('button', { name: 'Add' }))
  expect(models.getAllByText('gpt-two')).toHaveLength(1)

  // Blank input cannot be submitted.
  await user.type(models.getByLabelText('Add a model manually'), '   ')
  expect(models.getByRole('button', { name: 'Add' })).toBeDisabled()
  await user.clear(models.getByLabelText('Add a model manually'))

  // Surrounding whitespace is trimmed before adding the custom model.
  await user.type(models.getByLabelText('Add a model manually'), '  custom-lane  ')
  await user.click(models.getByRole('button', { name: 'Add' }))
  expect(models.getByText('custom-lane')).toBeVisible()
  expect(models.getByLabelText('Add a model manually')).toHaveValue('')
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
  const user = userEvent.setup()
  render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  fireEvent.change(screen.getByDisplayValue('https://saved.example'), {
    target: { value: 'https://draft.example' },
  })
  fireEvent.change(screen.getByLabelText('API Key *'), {
    target: { value: 'unsaved-key' },
  })
  await user.click(
    await screen.findByRole('button', { name: /Probe upstream models/ })
  )
  const dialog = await openDiscoveryDialog()
  expect(
    dialog.getByRole('checkbox', { name: 'preview-model' })
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
  await user.click(
    await screen.findByRole('button', { name: /Probe upstream models|Re-fetch/ })
  )
  const dialog = await openDiscoveryDialog()
  expect(
    dialog.getByRole('checkbox', { name: 'upstream-model' })
  ).toBeVisible()
  await user.click(dialog.getByRole('button', { name: 'Cancel' }))
  await waitFor(() =>
    expect(
      screen.queryByRole('dialog', { name: 'Select upstream models' })
    ).not.toBeInTheDocument()
  )
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
  const user = userEvent.setup()
  const view = render(<ConfigurationHarness currentRow={editingChannel} />)
  await screen.findByDisplayValue('Existing channel')
  await user.click(
    await screen.findByRole('button', { name: /Probe upstream models/ })
  )
  expect(await screen.findByText('Fetching models...')).toBeVisible()
  view.rerender(<ConfigurationHarness currentRow={otherChannel} />)
  await screen.findByDisplayValue('Second channel')
  await user.click(
    await screen.findByRole('button', { name: /Probe upstream models|Re-fetch/ })
  )
  const dialog = await openDiscoveryDialog()
  expect(
    dialog.getByRole('checkbox', { name: 'second-model' })
  ).toBeVisible()
  await act(async () => {
    reply.resolve({ data: { success: true, data: ['first-model'] } })
  })
  // The late first-channel response must not replace the second channel's
  // candidates inside the open picker.
  expect(
    dialog.queryByRole('checkbox', { name: 'first-model' })
  ).not.toBeInTheDocument()
  expect(dialog.getByRole('checkbox', { name: 'second-model' })).toBeVisible()
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
