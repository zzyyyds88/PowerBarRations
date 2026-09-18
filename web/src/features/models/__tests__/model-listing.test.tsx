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
  createRootRoute,
  createRoute,
  createRouter,
  createMemoryHistory,
  RouterProvider,
} from '@tanstack/react-router'
import {
  render,
  screen,
  within,
  cleanup,
  waitFor,
  act,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18n from 'i18next'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import fr from '@/i18n/locales/fr.json'
import zhCN from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { ModelsDialogs } from '../components/models-dialogs'
import { ModelsProvider } from '../components/models-provider'
import { ModelsTable } from '../components/models-table'
import type { Model } from '../types'

const metadata: Model = {
  id: 7,
  model_name: 'catalog-only',
  has_metadata: true,
  configured_channel_count: 0,
  name_rule: 0,
  status: 1,
  sync_official: 1,
  created_time: 1,
  updated_time: 1,
}
const channel: Model = {
  ...metadata,
  id: 0,
  model_name: 'channel-only',
  has_metadata: false,
  configured_channel_count: 1,
  status: 0,
  sync_official: 0,
}
const clients: QueryClient[] = []

function Page() {
  return (
    <ModelsProvider>
      <ModelsTable />
      <ModelsDialogs />
    </ModelsProvider>
  )
}

async function renderList(
  items: Model[] = [
    metadata,
    channel,
    { ...channel, model_name: 'other-channel' },
  ],
  options: { initialUrl?: string; total?: number } = {}
) {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (
      url === '/api/console/models/' ||
      url === '/api/console/models/search'
    ) {
      return {
        data: { items, total: options.total ?? items.length },
      }
    }
    if (url === '/api/console/models/7') {
      return { data: metadata }
    }
    return { data: { items: [] } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const root = createRootRoute()
  const authenticated = createRoute({
    getParentRoute: () => root,
    id: '_authenticated',
  })
  const models = createRoute({
    getParentRoute: () => authenticated,
    path: 'models/$section',
    component: Page,
  })
  const router = createRouter({
    routeTree: root.addChildren([authenticated.addChildren([models])]),
    history: createMemoryHistory({
      initialEntries: [options.initialUrl ?? '/models/metadata'],
    }),
  })
  await router.load()
  const result = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  if (items.length) {
    await screen.findByRole('button', { name: items[0].model_name })
  } else await screen.findByText('No Models Found')
  await waitFor(() => expect(client.isFetching()).toBe(0))
  return { ...result, get, router }
}

beforeEach(() => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'USD' },
  })
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  i18n.addResourceBundle('fr', 'translation', fr.translation, true, true)
  i18n.addResourceBundle('zhCN', 'translation', zhCN.translation, true, true)
})

afterEach(async () => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  await i18n.changeLanguage('en')
})

it('requests channel models and lists the model-catalog columns', async () => {
  const { get } = await renderList()
  expect(get).toHaveBeenCalledWith('/api/console/models/', {
    params: expect.objectContaining({ include_channel_models: true }),
  })
  expect(screen.getAllByText('Missing metadata')).toHaveLength(2)
  const headers = screen
    .getAllByRole('columnheader')
    .map((header) => header.textContent?.trim())
  expect(headers).toEqual([
    '',
    'Model',
    'Match Type',
    'Bound channels',
    'Description',
    'Tags',
    'Actions',
  ])
  for (const removed of [
    'Channels and groups',
    'Matched models',
    'Sync policy',
    'Display policy',
    'ID',
    'Custom endpoints',
    'Created',
    'Updated',
    'Vendor',
    'Available groups',
    'Billing type',
  ]) {
    expect(
      screen.queryByRole('columnheader', { name: removed })
    ).not.toBeInTheDocument()
  }
})

it('renders the inline rule hit and bound channel count for a rule row', async () => {
  await renderList([
    {
      ...metadata,
      model_name: 'qwen3-',
      name_rule: 1,
      matched_count: 2,
      matched_models: ['qwen3-max', 'qwen3-mini'],
      bound_channels: [
        { name: 'dashscope', type: 1 },
        { name: 'local', type: 2 },
      ],
    },
    metadata,
  ])
  // 规则行：模型名副行内联「Prefix · 2」，点击可看命中的具体模型名清单。
  expect(screen.getByText('Prefix')).toBeVisible()
  expect(screen.getByText(/Prefix ·/)).toBeVisible()
  const trigger = screen.getByRole('button', { name: 'View matched models' })
  expect(within(trigger).getByText('2')).toBeVisible()
  await userEvent.click(trigger)
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByText('qwen3-max')).toBeVisible()
  // 已绑定渠道列：计数来自后端 bound_channels（启用路由口径）。
  expect(screen.getByText('Channels 2')).toBeVisible()
  expect(screen.getByText('Channels 0')).toBeVisible()
  // 精确行：不出现内联命中（命中数恒为自身）。
  expect(screen.getAllByText(/·/)).toHaveLength(1)
})

it('keeps channel rows individually selectable and disables metadata mutations for mixed selection', async () => {
  await renderList()
  const user = userEvent.setup()
  await user.click(
    screen.getByRole('checkbox', { name: 'Select channel-only' })
  )
  expect(
    screen.getByRole('checkbox', { name: 'Select channel-only' })
  ).toBeChecked()
  expect(
    screen.getByRole('checkbox', { name: 'Select other-channel' })
  ).not.toBeChecked()
  await user.click(
    screen.getByRole('checkbox', { name: 'Select catalog-only' })
  )
  expect(
    screen.getByRole('checkbox', { name: 'Select channel-only' })
  ).toBeChecked()
  const toolbar = screen.getByRole('toolbar', { name: /Bulk actions/ })
  expect(
    within(toolbar).getByRole('button', { name: 'Delete selected models' })
  ).toBeDisabled()
  expect(
    within(toolbar).getByRole('button', { name: 'Copy model names' })
  ).toBeEnabled()
  await user.click(
    screen.getByRole('checkbox', { name: 'Select channel-only' })
  )
  expect(
    within(toolbar).getByRole('button', { name: 'Delete selected models' })
  ).toBeEnabled()
})

it('keeps long model names and the metadata hint truncated inside the model cell', async () => {
  const longName =
    'provider/very-long-channel-model-with-detailed-version-and-context-window'
  await renderList([
    { ...channel, model_name: longName },
    { ...metadata, status: 0 },
  ])
  expect(screen.getByText(longName)).toHaveClass('truncate')
  expect(screen.getByText('Add metadata')).toHaveClass('truncate')
  await act(async () => {
    await i18n.changeLanguage('fr')
  })
  await act(async () => {
    await i18n.changeLanguage('zhCN')
  })
  expect(screen.getByText('缺元数据')).toBeVisible()
  // 已绑定渠道列只带渠道数，不带分组（PBR 无用户分组）。
  expect(screen.getByText('已绑定渠道')).toBeVisible()
  expect(screen.queryByText(/分组/)).not.toBeInTheDocument()
})

it('prefills and creates metadata only when the user explicitly saves it', async () => {
  await renderList([channel])
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { ...channel, id: 9, has_metadata: true },
  })
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Add metadata' }))
  expect(screen.getByRole('textbox', { name: 'Model Name *' })).toHaveValue(
    'channel-only'
  )
  expect(post).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Save metadata' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/console/models/',
      expect.objectContaining({ model_name: 'channel-only' }),
      expect.anything()
    )
  )
})

it('no longer offers display-policy or sync-policy filters', async () => {
  await renderList([channel])
  expect(
    screen.queryByRole('button', { name: /Display policy/ })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: /Sync policy/ })
  ).not.toBeInTheDocument()
})

it('keeps the Tags column while collapsing its overflow', async () => {
  await renderList([
    {
      ...channel,
      tags: 'Tools,Files,Vision',
      bound_channels: [{ name: 'Main', type: 1 }],
      enable_groups: ['default', 'premium'],
    },
  ])
  expect(screen.getByRole('columnheader', { name: 'Tags' })).toBeVisible()
  expect(screen.queryByText('Channels 1 · Groups 2')).not.toBeInTheDocument()
  expect(screen.getByText('Tools')).toBeVisible()
  expect(screen.queryByText('Files')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Show all tags' }))
  expect(await screen.findByText('Files')).toBeVisible()
  expect(screen.getByText('Vision')).toBeVisible()
})
