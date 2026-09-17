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
/*
「路由与故障切换」独立页（ui-spec §6.3）：
- 集中列出全部路由键（explicit 可调用 / unconfigured 不可调用）与成员顺序摘要；
- 行内「编辑成员链」打开居中弹窗（固定模型模式，只显示该车道成员）。
测试资源为空表，i18n 文案即 key 本身。
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { Routes } from '..'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
    post: vi.fn(),
  },
}))

const mockedGet = vi.mocked(api.get)

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Routes />
    </QueryClientProvider>
  )
}

/** 两个路由键：model-1 已配车道（2 成员），model-2 未配车道（1 个候选渠道）。 */
function mockRouteKeys() {
  mockedGet.mockImplementation(async (url: string) => {
    if (url === '/api/v1/models') {
      return {
        data: {
          items: [
            {
              model: 'model-1',
              source: 'explicit',
              routable: true,
              member_count: 2,
            },
            {
              model: 'model-2',
              source: 'unconfigured',
              routable: false,
              member_count: 1,
            },
          ],
        },
      } as never
    }
    if (url === '/api/v1/lanes') {
      return {
        data: {
          items: [
            {
              name: 'model-1',
              members: [
                { channel: 'channel-a', upstream_model: 'model-1' },
                { channel: 'channel-b', upstream_model: 'model-1' },
              ],
            },
          ],
        },
      } as never
    }
    if (url === '/api/v1/routes/model-1') {
      return {
        data: {
          model: 'model-1',
          source: 'explicit',
          routable: true,
          mode: 'failover',
          members: [
            { channel: 'channel-a', upstream_model: 'model-1', priority: 2 },
            { channel: 'channel-b', upstream_model: 'model-1', priority: 1 },
          ],
        },
      } as never
    }
    throw new Error(`Unexpected GET ${url}`)
  })
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(() => {
  cleanup()
})

describe('路由与故障切换页', () => {
  test('列出全部路由键：状态徽章、成员数与顺序摘要', async () => {
    mockRouteKeys()
    renderPage()

    expect(await screen.findByText('model-1')).toBeInTheDocument()
    expect(screen.getByText('model-2')).toBeInTheDocument()
    // 状态语义：explicit 可调用 / unconfigured 不可调用。
    expect(screen.getByText('Callable')).toBeInTheDocument()
    expect(screen.getByText('Not callable')).toBeInTheDocument()
    // 成员数与顺序摘要（车道成员数组顺序即故障切换顺序）。
    expect(screen.getByText('2 members')).toBeInTheDocument()
    expect(screen.getByText('channel-a → channel-b')).toBeInTheDocument()
    // 未配车道：只有候选渠道数，无顺序。
    expect(screen.getByText('1 candidate channels')).toBeInTheDocument()
  })

  test('行内「编辑成员链」打开居中弹窗，只显示该车道成员', async () => {
    mockRouteKeys()
    renderPage()

    const user = userEvent.setup()
    const editButtons = await screen.findAllByRole('button', {
      name: 'Edit members',
    })
    await user.click(editButtons[0])

    const dialog = await screen.findByRole('dialog')
    expect(dialog).toBeVisible()
    // 固定模型模式：隐藏模型选择器与一键固化按钮，只渲染该模型成员链。
    expect(
      within(dialog).queryByText(/Routable models/)
    ).not.toBeInTheDocument()
    expect(
      within(dialog).queryByText('Generate missing lanes')
    ).not.toBeInTheDocument()
    // 弹窗标题与成员链编辑只含点中的模型（model-1），不含其他车道。
    expect(within(dialog).getAllByText('model-1').length).toBeGreaterThan(0)
    expect(within(dialog).queryByText('model-2')).not.toBeInTheDocument()
    expect(await within(dialog).findByText('channel-a')).toBeVisible()
    expect(within(dialog).getByText('channel-b')).toBeVisible()
  })

  test('空态：没有路由键时给出引导文案', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        return { data: { items: [] } } as never
      }
      if (url === '/api/v1/lanes') {
        return { data: { items: [] } } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    expect(await screen.findByText('No route keys yet')).toBeVisible()
    expect(
      screen.getByText(
        'Declare models on channels to see them here, then add members and save to create a lane.'
      )
    ).toBeVisible()
  })

  test('错误态：加载失败给出错误与重试入口', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        throw new Error('route keys unavailable')
      }
      if (url === '/api/v1/lanes') {
        return { data: { items: [] } } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    expect(await screen.findByText('route keys unavailable')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
  })
})
