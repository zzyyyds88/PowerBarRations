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
「路由与故障切换」独立页（ui-spec §6.3、ADR 0006/0007）：octopus 式卡片网格。
- **只列真实车道**（source ∈ explicit|disabled）：渠道声明但未配车道的路由键
  （unconfigured）不出现——渠道声明与车道彻底分列，删车道即卡片消失；
- 卡片操作固定为「编辑成员链 / 删除车道」；页头「新建车道」手填路由键。
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

/**
 * 三个路由键：model-1 已配车道（2 成员）、model-2 停用车道、model-3 渠道声明
 * 但未配车道（unconfigured，不得渲染）。
 */
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
              source: 'disabled',
              routable: false,
              member_count: 1,
              available_member_count: 1,
            },
            {
              model: 'model-3',
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
                {
                  channel: 'channel-a',
                  model: 'model-1',
                  upstream_model: 'model-1',
                },
                {
                  channel: 'channel-b',
                  model: 'model-1',
                  upstream_model: 'model-1',
                },
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
            {
              channel: 'channel-a',
              model: 'model-1',
              upstream_model: 'model-1',
              priority: 2,
            },
            {
              channel: 'channel-b',
              model: 'model-1',
              upstream_model: 'model-1',
              priority: 1,
            },
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
  test('只列真实车道：unconfigured 不出现在路由页', async () => {
    mockRouteKeys()
    renderPage()

    expect(await screen.findByText('model-1')).toBeInTheDocument()
    // 停用车道仍可见（source=disabled）。
    expect(screen.getByText('model-2')).toBeInTheDocument()
    expect(screen.getByText('Lane disabled')).toBeInTheDocument()
    // 渠道声明但未配车道的路由键不出现（ADR 0007）。
    expect(screen.queryByText('model-3')).not.toBeInTheDocument()
    expect(screen.queryByText(/candidate channels/)).not.toBeInTheDocument()
    // 状态语义：explicit 可调用。
    expect(screen.getByText('Callable')).toBeInTheDocument()
    // 成员数与顺序（车道成员数组顺序即故障转移顺序）。
    expect(screen.getByText('2 members')).toBeInTheDocument()
    expect(screen.getByText(/1\. channel-a/)).toBeInTheDocument()
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

  test('页头「新建车道」打开可编辑路由键的编排器，成员从空开始', async () => {
    mockRouteKeys()
    renderPage()

    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'New lane' }))
    const dialog = await screen.findByRole('dialog')
    const routeKey = within(dialog).getByLabelText('Route key')
    expect(routeKey).toBeEnabled()
    expect(routeKey).toHaveValue('')
    expect(within(dialog).getByText('No members yet')).toBeVisible()
  })

  test('每张车道卡片都提供删除入口，未配车道不再有新建按钮', async () => {
    mockRouteKeys()
    renderPage()

    const deleteButtons = await screen.findAllByRole('button', {
      name: 'Delete lane',
    })
    // model-1（explicit）+ model-2（disabled）各一个；unconfigured 不渲染。
    expect(deleteButtons).toHaveLength(2)
    expect(
      screen.queryByRole('button', { name: 'Create lane' })
    ).not.toBeInTheDocument()
  })

  test('空态：没有任何车道时给出引导文案', async () => {
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

    expect(await screen.findByText('No lanes yet')).toBeVisible()
    expect(
      screen.getByText(
        'Create a lane by hand: enter a route key and pick members from any channel. Declaring models on channels does not create lanes.'
      )
    ).toBeVisible()
  })

  test('成员顺序加载失败：内联错误与重试不影响车道列表', async () => {
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
              {
                channel: 'channel-a',
                model: 'model-1',
                upstream_model: 'model-1',
                priority: 2,
              },
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
                members: [
                  { channel: '', model: 'model-1', upstream_model: 'model-1' },
                ],
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

  // ui-spec §6.3：被人工停用的成员在卡片摘要里必须可见地标灰 + 短标记，
  // 否则卡片看起来"一切正常"而该成员实际不参与选路。
  test('卡片摘要把被人工停用的成员标出来', async () => {
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
                disabled_member_count: 1,
                degraded: false,
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
                  {
                    channel: 'channel-a',
                    model: 'model-1',
                    upstream_model: 'model-1',
                  },
                  {
                    channel: 'channel-b',
                    model: 'model-1',
                    upstream_model: 'model-1',
                    enabled: false,
                  },
                ],
              },
            ],
          },
        } as never
      }
      if (url === '/api/v1/lanes/model-1/health') {
        // 运行态不在本组用例职责内（见 lane-runtime.test.tsx）：抛错即"无运行态"，
        // 与既有用例同一约定，避免桩返回 null 让 hook 解构失败。
        throw new Error('health unavailable')
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    // 只给被停用的那一行加标记：停用数 1、成员 2，所以恰好一个徽章。
    const markers = await screen.findAllByText('Manually disabled')
    expect(markers).toHaveLength(1)
    // 标记落在 channel-b 那一行上（不是 channel-a）。
    expect(markers[0].closest('li')?.textContent).toContain('channel-b')
  })

  // api-spec §5.7 / ui-spec §6.3：「成员全被我关了」不得被「上游全挂了」冒充，
  // 两者的 degraded 都为 true，只能靠 disabled_member_count == member_count 区分。
  test('全部成员被人工关闭时用独立徽章，不冒充「全部成员不可用」', async () => {
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
                available_member_count: 0,
                disabled_member_count: 2,
                degraded: true,
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
                  {
                    channel: 'channel-a',
                    model: 'model-1',
                    upstream_model: 'model-1',
                    enabled: false,
                  },
                  {
                    channel: 'channel-b',
                    model: 'model-1',
                    upstream_model: 'model-1',
                    enabled: false,
                  },
                ],
              },
            ],
          },
        } as never
      }
      if (url === '/api/v1/lanes/model-1/health') {
        // 运行态不在本组用例职责内（见 lane-runtime.test.tsx）：抛错即"无运行态"，
        // 与既有用例同一约定，避免桩返回 null 让 hook 解构失败。
        throw new Error('health unavailable')
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    expect(await screen.findByText('All members disabled')).toBeVisible()
    expect(
      screen.queryByText('All members unavailable')
    ).not.toBeInTheDocument()
  })

  // 对照：上游全挂（无人为关闭）仍走「全部成员不可用」，两条路径文案不同。
  test('上游全挂时仍显示「全部成员不可用」', async () => {
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
                available_member_count: 0,
                disabled_member_count: 0,
                degraded: true,
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
                  {
                    channel: 'channel-a',
                    model: 'model-1',
                    upstream_model: 'model-1',
                  },
                  {
                    channel: 'channel-b',
                    model: 'model-1',
                    upstream_model: 'model-1',
                  },
                ],
              },
            ],
          },
        } as never
      }
      if (url === '/api/v1/lanes/model-1/health') {
        // 运行态不在本组用例职责内（见 lane-runtime.test.tsx）：抛错即"无运行态"，
        // 与既有用例同一约定，避免桩返回 null 让 hook 解构失败。
        throw new Error('health unavailable')
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    renderPage()

    expect(await screen.findByText('All members unavailable')).toBeVisible()
    expect(screen.queryByText('All members disabled')).not.toBeInTheDocument()
  })
})
