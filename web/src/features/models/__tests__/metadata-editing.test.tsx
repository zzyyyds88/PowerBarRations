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
import { render, screen, waitFor, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError } from 'axios'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { ModelMutateDrawer } from '../components/drawers/model-mutate-drawer'
import { ModelsProvider } from '../components/models-provider'

const model = {
  id: 7,
  model_name: 'example-model',
  description: 'Original',
  status: 1,
  sync_official: 1,
  name_rule: 0,
  vendor_id: 3,
  endpoints: '',
  supported_endpoints: ['openai'],
  created_time: 1,
  updated_time: 1,
}

afterEach(() => {
  cleanup()
  useAuthStore.getState().auth.reset()
})


describe('metadata editing', () => {
  it.each([
    {
      name: 'business rejection',
      response: { success: false, message: '模型名称已存在' },
    },
    { name: 'HTTP rejection', response: null },
  ])(
    'shows the server reason for a $name and preserves the draft for retry',
    async ({ response }) => {
      useAuthStore
        .getState()
        .auth.setUser({ id: 2, username: 'admin', role: 10 })
      vi.spyOn(api, 'get').mockResolvedValue({
        data: { success: true, data: { items: [] } },
      })
      const post = vi.spyOn(api, 'post')
      if (response) {
        post.mockResolvedValueOnce({ data: response })
      } else {
        const error = new AxiosError('Request failed with status code 409')
        error.response = {
          data: { message: '模型名称已存在' },
          status: 409,
          statusText: 'Conflict',
          headers: {},
          config: { headers: {} },
        } as typeof error.response
        post.mockRejectedValueOnce(error)
      }
      post.mockResolvedValue({ data: { success: true } })
      const close = vi.fn()
      const fallbackError = vi.fn()
      const client = new QueryClient({
        defaultOptions: {
          queries: { retry: false },
          mutations: { retry: false, onError: fallbackError },
        },
      })
      render(
        <QueryClientProvider client={client}>
          <ModelMutateDrawer open onOpenChange={close} />
        </QueryClientProvider>
      )
      const user = userEvent.setup()
      await user.type(screen.getByLabelText('Model Name *'), 'duplicate-model')
      await user.type(screen.getByLabelText('Description'), 'Keep this draft')
      await user.click(screen.getByRole('button', { name: 'Save metadata' }))
      expect(await screen.findByRole('alert')).toHaveTextContent(
        '模型名称已存在'
      )
      expect(fallbackError).not.toHaveBeenCalled()
      expect(close).not.toHaveBeenCalled()
      expect(screen.getByLabelText('Model Name *')).toHaveValue(
        'duplicate-model'
      )
      expect(screen.getByLabelText('Description')).toHaveValue(
        'Keep this draft'
      )
      await user.clear(screen.getByLabelText('Model Name *'))
      await user.type(screen.getByLabelText('Model Name *'), 'unique-model')
      await user.click(screen.getByRole('button', { name: 'Save metadata' }))
      await waitFor(() => expect(close).toHaveBeenCalledWith(false))
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      expect(post).toHaveBeenLastCalledWith(
        '/api/console/models/',
        expect.objectContaining({
          model_name: 'unique-model',
          description: 'Keep this draft',
        }),
        { skipBusinessError: true, skipErrorHandler: true }
      )
      client.clear()
    }
  )

  it('allows an administrator to save metadata without loading or changing system pricing', async () => {
    useAuthStore.getState().auth.setUser({ id: 2, username: 'admin', role: 10 })
    const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/console/models/7') {
        return { data: { success: true, data: model } }
      }
      if (url === '/api/vendors/') {
        return {
          data: {
            success: true,
            data: {
              items: [
                { id: 3, name: 'Existing vendor', icon: 'Gemini.Color' },
                { id: 4, name: 'Another vendor', icon: 'Gemini.Color' },
              ],
            },
          },
        }
      }
      return { data: { success: false, message: 'Root only' } }
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: model } })
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    render(
      <QueryClientProvider client={client}>
        <ModelsProvider>
          <ModelMutateDrawer open onOpenChange={() => {}} currentRow={model} />
        </ModelsProvider>
      </QueryClientProvider>
    )
    const description = await screen.findByLabelText('Description')
    await waitFor(() => expect(description).toHaveValue('Original'))
    expect(screen.getByRole('combobox', { name: 'Vendor' })).toHaveValue(
      'Existing vendor'
    )
    const user = userEvent.setup()
    expect(screen.getByText('Gemini.Color')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Custom model icon' }))
    const icon = screen.getByRole('combobox', { name: 'Icon' })
    await user.type(icon, 'Claude.Avatar')
    await user.keyboard('{Escape}')
    expect(screen.getByText('Claude.Avatar')).toBeVisible()
    await user.click(
      screen.getByRole('button', { name: 'Inherit vendor icon' })
    )
    expect(
      screen.queryByRole('combobox', { name: 'Icon' })
    ).not.toBeInTheDocument()
    expect(screen.getByText('Gemini.Color')).toBeVisible()
    const vendorInput = screen.getByRole('combobox', { name: 'Vendor' })
    await user.click(vendorInput)
    await user.type(vendorInput, 'Another')
    await user.click(screen.getByRole('option', { name: 'Another vendor' }))
    expect(vendorInput).toHaveValue('Another vendor')
    await user.clear(description)
    await user.type(description, 'Updated metadata')
    await user.click(
      screen.getByRole('button', { name: /Update Model|Save metadata/ })
    )
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(
      get.mock.calls.some(([url]) => String(url).startsWith('/api/option'))
    ).toBe(false)
    expect(put.mock.calls.every(([url]) => url === '/api/console/models/')).toBe(true)
    expect(put.mock.calls[0][1]).toMatchObject({
      description: 'Updated metadata',
      icon: '',
      model_name: 'example-model',
      vendor_id: 4,
      endpoints: '',
    })
  })
})
