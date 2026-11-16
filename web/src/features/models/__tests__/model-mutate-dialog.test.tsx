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
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { ModelMutateDrawer } from '../components/drawers/model-mutate-drawer'
import { ModelsProvider } from '../components/models-provider'
import type { Model } from '../types'

const model: Model = {
  id: 7,
  model_name: 'deepseek-v4-flash',
  description: 'Original',
  status: 1,
  sync_official: 1,
  name_rule: 0,
  endpoints: '',
  created_time: 1,
  updated_time: 1,
}

function renderDialog(onOpenChange = vi.fn()) {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/console/models/7') {
      return { data: model }
    }
    if (url === '/api/channel/search') {
      return { data: { items: [], total: 0 } }
    }
    return { data: { items: [] } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.setQueryData(['status'], {})
  render(
    <QueryClientProvider client={client}>
      <ModelsProvider>
        <ModelMutateDrawer
          open
          onOpenChange={onOpenChange}
          currentRow={model}
        />
      </ModelsProvider>
    </QueryClientProvider>
  )
  return { onOpenChange, client }
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
})
afterEach(() => {
  cleanup()
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
})

it('opens the edit model form as a centered dialog instead of a side sheet', async () => {
  renderDialog()
  await waitFor(() =>
    expect(screen.getByLabelText('Model Name *')).toHaveValue(
      'deepseek-v4-flash'
    )
  )
  const dialog = screen.getByRole('dialog')
  expect(dialog).toHaveAttribute('data-slot', 'dialog-content')
  expect(dialog.className).toContain('w-[min(94vw,960px)]')
  expect(screen.queryByRole('dialog', { hidden: false })).toBeTruthy()
  expect(document.querySelector('[data-slot="sheet-content"]')).toBeNull()
})

it('keeps only the basic information and channel association sections', async () => {
  renderDialog()
  await waitFor(() =>
    expect(screen.getByText('Basic Information')).toBeVisible()
  )
  expect(screen.getByText('Channel association')).toBeVisible()
  for (const removed of [
    'Matching Rules',
    'Endpoints',
    'Status & Sync',
    'Sync policy',
    'Display policy',
    'Custom endpoints',
  ]) {
    expect(screen.queryByText(removed)).not.toBeInTheDocument()
  }
})

it('offers the four-level match type selector defaulting to exact', async () => {
  renderDialog()
  const trigger = await screen.findByRole('combobox', { name: 'Match Type' })
  expect(trigger).toHaveTextContent('Exact')
  expect(screen.getByText('Match model name exactly')).toBeVisible()
  await userEvent.click(trigger)
  const listbox = await screen.findByRole('listbox')
  expect(
    within(listbox)
      .getAllByRole('option')
      .map((option) => option.textContent)
  ).toEqual(['Exact', 'Prefix', 'Contains', 'Suffix'])
})

it('explains auto-matching when a non-exact rule is selected and submits name_rule', async () => {
  renderDialog()
  const user = userEvent.setup()
  const trigger = await screen.findByRole('combobox', { name: 'Match Type' })
  expect(
    screen.queryByText(/automatically matches model names declared by channels/)
  ).not.toBeInTheDocument()
  await user.click(trigger)
  await user.click(await screen.findByRole('option', { name: 'Prefix' }))
  // 选中非精确档后，字段下方出现"自动命中渠道声明的模型名"提示。
  expect(
    screen.getByText(
      'This rule automatically matches model names declared by channels by prefix, contains or suffix (no need to add them one by one).'
    )
  ).toBeVisible()
  expect(trigger).toHaveTextContent('Prefix')
  const put = vi.spyOn(api, 'put').mockResolvedValue({
    data: { ...model, name_rule: 1 },
  })
  await user.click(screen.getByRole('button', { name: 'Save metadata' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/console/models/',
      expect.objectContaining({ id: 7, name_rule: 1 }),
      expect.anything()
    )
  )
})

it('preselects the stored rule when editing a matching-rule record', async () => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/console/models/7') {
      return { data: { ...model, name_rule: 3 } }
    }
    if (url === '/api/channel/search') {
      return { data: { items: [], total: 0 } }
    }
    return { data: { items: [] } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ModelsProvider>
        <ModelMutateDrawer open onOpenChange={vi.fn()} currentRow={model} />
      </ModelsProvider>
    </QueryClientProvider>
  )
  const trigger = await screen.findByRole('combobox', { name: 'Match Type' })
  await waitFor(() => expect(trigger).toHaveTextContent('Suffix'))
  expect(screen.getByText('Match models ending with this name')).toBeVisible()
})

it('auto-applies the icon detected from the model name', async () => {
  renderDialog()
  await waitFor(() =>
    expect(screen.getByLabelText('Model Name *')).toHaveValue(
      'deepseek-v4-flash'
    )
  )
  // ui-spec §6.3：识别到就自动采用，不再需要点「生效图标」。
  const icon = screen.getByRole('combobox', { name: 'Icon' })
  await waitFor(() => expect(icon).toHaveValue('DeepSeek.Color'))
  expect(screen.getByText('Effective icon')).toBeVisible()
  expect(screen.getAllByText('DeepSeek.Color').length).toBeGreaterThan(0)
})

it('re-detects and re-applies the icon when the model name changes', async () => {
  renderDialog()
  const user = userEvent.setup()
  const nameInput = await screen.findByLabelText('Model Name *')
  await waitFor(() => expect(nameInput).toHaveValue('deepseek-v4-flash'))
  const icon = screen.getByRole('combobox', { name: 'Icon' })
  await waitFor(() => expect(icon).toHaveValue('DeepSeek.Color'))

  await user.clear(nameInput)
  await user.type(nameInput, 'qwen3.8-flash')
  await waitFor(() => expect(icon).toHaveValue('Qwen.Color'))
})

it('discards unsaved drafts only after confirmation when closing', async () => {
  const { onOpenChange } = renderDialog()
  const user = userEvent.setup()
  await waitFor(() =>
    expect(screen.getByLabelText('Description')).toHaveValue('Original')
  )
  await user.clear(screen.getByLabelText('Description'))
  await user.type(screen.getByLabelText('Description'), 'Changed')
  await user.keyboard('{Escape}')
  expect(await screen.findByText('Discard unsaved changes?')).toBeVisible()
  expect(onOpenChange).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Discard changes' }))
  await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
})
