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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { NewLaneDialog } from '../new-lane-dialog'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), put: vi.fn(), delete: vi.fn(), post: vi.fn() },
}))

const mockedGet = vi.mocked(api.get)
const mockedPut = vi.mocked(api.put)

function renderDialog() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <NewLaneDialog open onOpenChange={() => {}} />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  mockedPut.mockResolvedValue({ data: {} } as never)
})

describe('新建车道', () => {
  test('输入任意路由键并勾选成员后保存为车道', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        return { data: { items: [] } } as never
      }
      if (url.startsWith('/api/v1/routes/')) {
        return {
          data: {
            model: 'custom-key',
            source: 'unconfigured',
            routable: false,
            members: [
              {
                channel_id: 1,
                channel: 'channel-a',
                upstream_model: 'real-a',
                priority: 1,
              },
            ],
          },
        } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    const user = userEvent.setup()
    renderDialog()

    await user.type(screen.getByLabelText('Route key'), 'custom-key')
    // 候选渠道以「添加」按钮呈现。
    await user.click(await screen.findByRole('button', { name: /channel-a/ }))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
    const [url, body] = mockedPut.mock.calls[0] as [
      string,
      { members: { channel: string; priority: number }[] },
    ]
    expect(url).toBe('/api/v1/lanes/custom-key')
    expect(body.members).toEqual([
      { channel: 'channel-a', upstream_model: 'real-a', priority: 1 },
    ])
  })

  test('成员可排序、可改上游真名，顺序与 upstream_model 随保存提交', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        return { data: { items: [] } } as never
      }
      if (url.startsWith('/api/v1/routes/')) {
        return {
          data: {
            model: 'pooled',
            source: 'unconfigured',
            routable: false,
            members: [
              {
                channel_id: 1,
                channel: 'channel-a',
                upstream_model: 'real-a',
                priority: 2,
              },
              {
                channel_id: 2,
                channel: 'channel-b',
                upstream_model: 'real-b',
                priority: 1,
              },
            ],
          },
        } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    const user = userEvent.setup()
    renderDialog()

    await user.type(screen.getByLabelText('Route key'), 'pooled')
    await user.click(await screen.findByRole('button', { name: /channel-a/ }))
    await user.click(await screen.findByRole('button', { name: /channel-b/ }))
    // 把第二个成员上移，并给它改名。
    await user.click(screen.getAllByRole('button', { name: 'Move up' })[1])
    const renameInput = screen.getByLabelText('Upstream model for channel-b')
    await user.clear(renameInput)
    await user.type(renameInput, 'vendor-b')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
    const [, body] = mockedPut.mock.calls[0] as [
      string,
      {
        members: { channel: string; upstream_model: string; priority: number }[]
      },
    ]
    expect(body.members.map((m) => m.channel)).toEqual([
      'channel-b',
      'channel-a',
    ])
    expect(body.members[0].upstream_model).toBe('vendor-b')
    expect(body.members[0].priority).toBeGreaterThan(body.members[1].priority)
  })
})
