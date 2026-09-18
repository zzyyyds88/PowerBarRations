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
import { AxiosError } from 'axios'
import { Toaster, toast } from 'sonner'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { FetchModelsDialog } from '../fetch-models-dialog'

let client: QueryClient

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
})

afterEach(() => {
  cleanup()
  toast.dismiss()
  client.clear()
  vi.restoreAllMocks()
})

function upstreamError(message: string): AxiosError {
  const error = new AxiosError(message)
  error.response = {
    data: { error: { code: 'upstream_error', message } },
    status: 502,
    statusText: 'Bad Gateway',
    headers: {},
    config: { headers: {} },
  } as typeof error.response
  return error
}

test('opening the dialog fetches upstream models, checks the saved list and hands the checked set to the form', async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(['gpt-existing', 'gpt-new', 'alias'])
  const select = vi.fn()
  const close = vi.fn()
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={client}>
      <Toaster />
      <FetchModelsDialog
        open
        onOpenChange={close}
        customFetcher={fetcher}
        onModelsSelected={select}
        channelName='Demo channel'
        existingModels={['gpt-existing', 'manual-model']}
        redirectSourceModels={['alias']}
      />
    </QueryClientProvider>
  )

  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1))
  const dialog = await screen.findByRole('dialog', { name: 'Fetch Models' })
  expect(within(dialog).getByText(/Demo channel/)).toBeVisible()

  // Saved models arrive checked under the Existing tab; upstream additions are
  // unchecked under the New tab.
  await user.click(
    await within(dialog).findByRole('tab', { name: 'Existing Models (1)' })
  )
  expect(
    await within(dialog).findByRole('checkbox', { name: 'gpt-existing' })
  ).toBeChecked()

  await user.click(within(dialog).getByRole('tab', { name: 'New Models (2)' }))
  const added = within(dialog).getByRole('checkbox', { name: 'gpt-new' })
  expect(added).not.toBeChecked()

  await user.click(added)
  await user.click(within(dialog).getByRole('button', { name: 'Save Models' }))

  expect(select).toHaveBeenCalledWith([
    'gpt-existing',
    'manual-model',
    'gpt-new',
  ])
  expect(close).toHaveBeenCalledWith(false)
  expect(await screen.findByText('Models filled to form')).toBeVisible()
})

test('a failed upstream fetch surfaces the server message and Retry re-fetches', async () => {
  const fetcher = vi
    .fn()
    .mockRejectedValueOnce(upstreamError('Upstream rejected the key'))
    .mockResolvedValueOnce(['upstream-new'])
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={client}>
      <Toaster />
      <FetchModelsDialog
        open
        onOpenChange={vi.fn()}
        customFetcher={fetcher}
        onModelsSelected={vi.fn()}
        existingModels={[]}
      />
    </QueryClientProvider>
  )

  const dialog = await screen.findByRole('dialog', { name: 'Fetch Models' })
  expect(
    await within(dialog).findByText('Upstream rejected the key')
  ).toBeVisible()
  expect(
    within(dialog).queryByRole('button', { name: 'Save Models' })
  ).not.toBeInTheDocument()

  await user.click(within(dialog).getByRole('button', { name: 'Retry' }))
  expect(fetcher).toHaveBeenCalledTimes(2)
  expect(
    await within(dialog).findByRole('checkbox', { name: 'upstream-new' })
  ).toBeVisible()
  expect(
    within(dialog).getByRole('button', { name: 'Save Models' })
  ).toBeVisible()
})

test('without a form callback the dialog saves the checked models to the saved channel', async () => {
  const get = vi
    .spyOn(api, 'get')
    .mockResolvedValue({ data: ['gpt-a', 'gpt-b'] })
  const put = vi.spyOn(api, 'put').mockResolvedValue({ data: { id: 42 } })
  const close = vi.fn()
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={client}>
      <Toaster />
      <FetchModelsDialog
        open
        onOpenChange={close}
        channelId={42}
        channelName='Saved channel'
        existingModels={['gpt-a']}
      />
    </QueryClientProvider>
  )

  const dialog = await screen.findByRole('dialog', { name: 'Fetch Models' })
  await waitFor(() =>
    expect(get).toHaveBeenCalledWith(
      '/api/channel/fetch_models/42',
      expect.anything()
    )
  )
  await user.click(within(dialog).getByRole('button', { name: 'Save Models' }))

  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/channel/',
      { id: 42, models: 'gpt-a' },
      expect.anything()
    )
  )
  expect(close).toHaveBeenCalledWith(false)
  expect(await screen.findByText('Models updated successfully')).toBeVisible()
})
