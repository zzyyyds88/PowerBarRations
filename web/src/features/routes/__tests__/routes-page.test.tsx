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
「路由与故障切换」独立页（ui-spec §6.3、ADR 0006）：octopus 式卡片网格。
- 集中列出全部路由键（explicit 可调用 / unconfigured 不可调用）与成员顺序；
- 卡片操作「编辑成员链」打开两栏编排器；未配车道时显示「新建车道」入口；
- 渠道声明/新增模型不会自动建车道。测试资源为空表，i18n 文案即 key 本身。
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
  getFreshAuthHeaders: vi.fn(async () => ({})),
}))

// SSE 长连接与真实 fetch 不在本页用例职责内（运行态渲染见 lane-runtime.test.tsx）：
// 这里只桩掉连接器，保留帧解析/对账等纯逻辑为真实实现。
vi.mock('@/lib/route-events', async (importOriginal) => {
  const mod = (await importOriginal()) as typeof import('@/lib/route-events')
  return { ...mod, openRouteEventStream: vi.fn(() => ({ close: vi.fn() })) }
})

const mockedGet = vi.mocked(api.get)
const mockedDelete = vi.mocked(api.delete)

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
              available_member_count: 1,
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
    if (url === '/api/v1/lane-summaries') {
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
    if (url === '/api/v1/channels') {
      return { data: { items: [] } } as never
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
  test('以卡片网格列出全部路由键：状态徽章、成员数与顺序', async () => {
    mockRouteKeys()
    renderPage()

    expect(await screen.findByText('model-1')).toBeInTheDocument()
    expect(screen.getByText('model-2')).toBeInTheDocument()
    // 状态语义：explicit 可调用 / unconfigured 不可调用。
    expect(screen.getByText('Callable')).toBeInTheDocument()
    expect(screen.getByText('Not callable')).toBeInTheDocument()
    // 成员数与顺序（车道成员数组顺序即故障转移顺序）。
    expect(screen.getByText('2 members')).toBeInTheDocument()
    expect(screen.getByText(/1\. channel-a/)).toBeInTheDocument()
    // 未配车道：候选渠道数 + 不可调用提示（同一 span 内拼接）。
    expect(screen.getByText(/1 candidate channels/)).toBeInTheDocument()
  })

  test('卡片「编辑成员链」打开两栏编排器并载入该车道成员', async () => {
    mockRouteKeys()
    renderPage()

    const user = userEvent.setup()
    const editButtons = await screen.findAllByRole('button', {
      name: 'Edit members',
    })
    await user.click(editButtons[0])

    const dialog = await screen.findByRole('dialog')
    expect(dialog).toBeVisible()
    expect(await within(dialog).findByText('channel-a')).toBeVisible()
    expect(within(dialog).getByText('channel-b')).toBeVisible()
    // 编辑既有车道：路由键只读。
    expect(within(dialog).getByLabelText('Route key')).toBeDisabled()
  })

  test('未配车道的卡片打开可编辑且预填路由键的编排器', async () => {
    mockRouteKeys()
    renderPage()

    const user = userEvent.setup()
    const createButtons = await screen.findAllByRole('button', {
      name: 'Create lane',
    })
    // model-2 是未配车道的那张卡片。
    await user.click(createButtons[0])

    const dialog = await screen.findByRole('dialog')
    const routeKey = within(dialog).getByLabelText('Route key')
    // 未配车道：新建模式，路由键可编辑且预填该模型名（ADR 0006）。
    expect(routeKey).toBeEnabled()
    expect(routeKey).toHaveValue('model-2')
    // 未配车道不得自动填入推荐成员。
    expect(within(dialog).getByText('No members yet')).toBeVisible()
  })

  test('页头「新建车道」打开可编辑路由键的编排器', async () => {
    mockRouteKeys()
    renderPage()

    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'New lane' }))
    const dialog = await screen.findByRole('dialog')
    const routeKey = within(dialog).getByLabelText('Route key')
    expect(routeKey).toBeEnabled()
    expect(routeKey).toHaveValue('')
  })

  test('空态：没有路由键时给出引导文案', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        return { data: { items: [] } } as never
      }
      if (url === '/api/v1/lane-summaries') {
        return { data: { items: [] } } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    expect(await screen.findByText('No route keys yet')).toBeVisible()
    expect(
      screen.getByText(
        'Create a lane by hand and pick members from any channel to make a route key callable.'
      )
    ).toBeVisible()
  })

  test('成员顺序加载失败：内联错误与重试不影响模型列表', async () => {
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
            ],
          },
        } as never
      }
      if (url === '/api/v1/lane-summaries') {
        throw new Error('lane order unavailable')
      }
      if (url === '/api/v1/routes/model-1') {
        return {
          data: {
            model: 'model-1',
            source: 'explicit',
            routable: true,
            members: [
              { channel: 'channel-a', upstream_model: 'model-1', priority: 2 },
            ],
          },
        } as never
      }
      if (url === '/api/v1/channels') {
        return { data: { items: [] } } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    // 主列表仍然可用。
    expect(await screen.findByText('model-1')).toBeInTheDocument()
    // 成员顺序有独立的内联错误与重试入口。
    expect(
      await screen.findByText('Failed to load lane member order')
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
  })

  test('错误态：加载失败给出错误与重试入口', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        throw new Error('route keys unavailable')
      }
      if (url === '/api/v1/lane-summaries') {
        return { data: { items: [] } } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    expect(await screen.findByText('route keys unavailable')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
  })

  test('成员列标注不可用成员数', async () => {
    mockRouteKeys()
    renderPage()

    expect(await screen.findByText('2 members')).toBeVisible()
    expect(screen.getByText('(1 unavailable)')).toBeVisible()
  })

  test('成员全不可用时状态显示全部不可用', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        return {
          data: {
            items: [
              {
                model: 'model-1',
                source: 'explicit',
                routable: true,
                member_count: 1,
                available_member_count: 1,
                healthy_member_count: 0,
                health_member_count: 1,
                degraded: true,
              },
            ],
          },
        } as never
      }
      if (url === '/api/v1/lane-summaries') {
        return { data: { items: [] } } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    expect(await screen.findByText('All members unavailable')).toBeVisible()
    expect(screen.queryByText('Callable')).not.toBeInTheDocument()
  })

  test('悬空成员给出清理入口', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/models') {
        return {
          data: {
            items: [
              {
                model: 'model-1',
                source: 'explicit',
                routable: true,
                member_count: 1,
                available_member_count: 0,
              },
            ],
          },
        } as never
      }
      if (url === '/api/v1/lane-summaries') {
        return {
          data: {
            items: [
              {
                name: 'model-1',
                orphan_member_count: 1,
                members: [{ channel: '', upstream_model: 'model-1' }],
              },
            ],
          },
        } as never
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    const user = userEvent.setup()
    renderPage()

    expect(await screen.findByText('Clean up orphan members')).toBeVisible()
    await user.click(
      screen.getByRole('button', { name: 'Clean up orphan members' })
    )
    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText(/model-1/)).toBeVisible()
  })

  test('删除车道走二次确认', async () => {
    mockRouteKeys()
    mockedDelete.mockResolvedValue({ data: {} } as never)
    const user = userEvent.setup()
    renderPage()

    const deleteButtons = await screen.findAllByRole('button', {
      name: 'Delete lane',
    })
    await user.click(deleteButtons[0])

    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText('Remove this lane?')).toBeVisible()
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(mockedDelete).not.toHaveBeenCalled()
  })
})
