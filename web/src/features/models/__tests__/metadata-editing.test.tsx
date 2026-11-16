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
      name: 'error envelope rejection',
      envelope: {
        error: { code: 'conflict', message: '模型名称已存在' },
      },
    },
    { name: 'flat message rejection', envelope: { message: '模型名称已存在' } },
  ])(
    'shows the server reason for a $name and preserves the draft for retry',
    async ({ envelope }) => {
      useAuthStore
        .getState()
        .auth.setUser({ id: 2, username: 'admin', role: 10 })
      vi.spyOn(api, 'get').mockResolvedValue({
        data: { items: [] },
      })
      const post = vi.spyOn(api, 'post')
      // 新契约：失败一律是非 2xx + 错误包络，axios 直接拒绝。
      const error = new AxiosError('Request failed with status code 409')
      error.response = {
        data: envelope,
        status: 409,
        statusText: 'Conflict',
        headers: {},
        config: { headers: {} },
      } as typeof error.response
      post.mockRejectedValueOnce(error)
      post.mockResolvedValue({ data: model })
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
        { skipErrorHandler: true }
      )
      client.clear()
    }
  )

  it('allows an administrator to save metadata without writing system pricing', async () => {
    useAuthStore.getState().auth.setUser({ id: 2, username: 'admin', role: 10 })
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/console/models/7') {
        return { data: model }
      }
      if (url === '/api/option/') {
        return { data: [] }
      }
      if (url === '/api/channel/search') {
        return { data: { items: [], total: 0 } }
      }
      throw new AxiosError('Root only')
    })
    const put = vi.spyOn(api, 'put').mockResolvedValue({ data: model })
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
    const user = userEvent.setup()
    const icon = screen.getByRole('combobox', { name: 'Icon' })
    await user.type(icon, 'Claude.Avatar')
    await user.keyboard('{Escape}')
    expect(screen.getByText('Claude.Avatar')).toBeVisible()
    await user.clear(description)
    await user.type(description, 'Updated metadata')
    await user.click(
      screen.getByRole('button', { name: /Update Model|Save metadata/ })
    )
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(
      put.mock.calls.some(([url]) => String(url).startsWith('/api/option'))
    ).toBe(false)
    expect(
      put.mock.calls.every(([url]) => url === '/api/console/models/')
    ).toBe(true)
    expect(put.mock.calls[0][1]).toMatchObject({
      description: 'Updated metadata',
      icon: 'Claude.Avatar',
      model_name: 'example-model',
      endpoints: '',
    })
  })
})
