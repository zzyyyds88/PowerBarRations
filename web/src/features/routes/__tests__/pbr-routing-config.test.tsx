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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { savePBRFailover } from '../api'
import { ModelRoutingPanel } from '../components/model-routing-panel'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
    post: vi.fn(),
  },
}))

const mockedGet = vi.mocked(api.get)
const mockedPut = vi.mocked(api.put)

function renderPanel(initialModel?: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ModelRoutingPanel initialModel={initialModel} />
    </QueryClientProvider>
  )
}

/** 已配车道：两个成员，a 在前。 */
function mockConfiguredLane() {
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
    if (url === '/api/v1/routes/model-1') {
      return {
        data: {
          model: 'model-1',
          source: 'explicit',
          routable: true,
          mode: 'failover',
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
}

/** 未配车道：建议链给出两个候选，成员列表为空。 */
function mockUnconfiguredModel() {
  mockedGet.mockImplementation(async (url: string) => {
    if (url === '/api/v1/models') {
      return {
        data: {
          items: [
            {
              model: 'model-1',
              source: 'unconfigured',
              routable: false,
              member_count: 2,
            },
          ],
        },
      } as never
    }
    if (url === '/api/v1/routes/model-1') {
      return {
        data: {
          model: 'model-1',
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
}

beforeEach(() => {
  vi.clearAllMocks()
  mockedPut.mockResolvedValue({ data: {} } as never)
})

describe('成员链手工管理', () => {
  test('删除成员后保存只提交剩余成员且顺序即优先级', async () => {
    mockConfiguredLane()
    const user = userEvent.setup()
    renderPanel('model-1')

    expect(await screen.findByText('channel-a')).toBeInTheDocument()
    await user.click(
      screen.getAllByRole('button', { name: 'Remove member' })[0]
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { members: { channel: string; priority: number }[] },
    ]
    expect(body.members.map((m) => m.channel)).toEqual(['channel-b'])
    expect(body.members[0].priority).toBe(1)
  })

  test('上移成员后保存按新顺序生成递减优先级', async () => {
    mockConfiguredLane()
    const user = userEvent.setup()
    renderPanel('model-1')

    expect(await screen.findByText('channel-a')).toBeInTheDocument()
    await user.click(screen.getAllByRole('button', { name: 'Move down' })[0])
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { members: { channel: string; priority: number }[] },
    ]
    expect(body.members.map((m) => m.channel)).toEqual([
      'channel-b',
      'channel-a',
    ])
    expect(body.members.map((m) => m.priority)).toEqual([2, 1])
  })

  test('编辑上游真名后保存提交成员级覆盖', async () => {
    mockConfiguredLane()
    const user = userEvent.setup()
    renderPanel('model-1')

    expect(await screen.findByText('channel-a')).toBeInTheDocument()
    await user.type(
      screen.getByLabelText('Upstream model for channel-a'),
      'renamed-a'
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { members: { channel: string; upstream_model?: string }[] },
    ]
    expect(body.members[0]).toMatchObject({
      channel: 'channel-a',
      upstream_model: 'renamed-a',
    })
  })

  test('未配车道时成员列表为空，添加候选后才可保存', async () => {
    mockUnconfiguredModel()
    const user = userEvent.setup()
    renderPanel('model-1')

    expect(await screen.findByText('No members yet')).toBeInTheDocument()
    const save = screen.getByRole('button', { name: 'Save' })
    expect(save).toBeDisabled()

    const candidateArea = screen
      .getByText('Candidate channels (declared in channels)')
      .closest('div')
    expect(candidateArea).not.toBeNull()
    await user.click(
      within(candidateArea as HTMLElement).getByRole('button', {
        name: /channel-a/,
      })
    )
    await waitFor(() => expect(save).toBeEnabled())
    await user.click(save)

    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { members: { channel: string; priority: number }[] },
    ]
    expect(body.members.map((m) => m.channel)).toEqual(['channel-a'])
  })

  test('空成员链阻止保存', async () => {
    mockConfiguredLane()
    const user = userEvent.setup()
    renderPanel('model-1')

    expect(await screen.findByText('channel-a')).toBeInTheDocument()
    await user.click(
      screen.getAllByRole('button', { name: 'Remove member' })[0]
    )
    await user.click(
      screen.getAllByRole('button', { name: 'Remove member' })[0]
    )

    expect(screen.getByText('No members yet')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(mockedPut).not.toHaveBeenCalled()
  })
})

describe('savePBRFailover 六键来源', () => {
  test('新车道用系统设置里的默认六键（不写死前端默认值）', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/system/options') {
        return {
          data: {
            lane_defaults: {
              member_max_attempts: 5,
              member_retry_interval_seconds: 0,
              member_non_stream_response_timeout_seconds: 90,
              member_stream_first_event_timeout_seconds: 15,
              member_cooldown_seconds: 30,
              member_affinity_seconds: 7,
            },
          },
        } as never
      }
      // 车道不存在 → 404 风格的失败
      throw new Error('not found')
    })
    mockedPut.mockResolvedValue({ data: {} } as never)

    await savePBRFailover('model-1', [{ channel: 'channel-a', priority: 10 }])

    expect(mockedPut).toHaveBeenCalledWith('/api/v1/lanes/model-1', {
      enabled: true,
      mode: 'failover',
      config: {
        member_max_attempts: 5,
        member_retry_interval_seconds: 0,
        member_non_stream_response_timeout_seconds: 90,
        member_stream_first_event_timeout_seconds: 15,
        member_cooldown_seconds: 30,
        member_affinity_seconds: 7,
      },
      members: [{ channel: 'channel-a', priority: 10 }],
    })
  })

  test('已有车道保留其自身六键，只改成员顺序', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/system/options') {
        return { data: { lane_defaults: { member_max_attempts: 5 } } } as never
      }
      return {
        data: {
          config: {
            member_max_attempts: 1,
            member_retry_interval_seconds: 0,
            member_non_stream_response_timeout_seconds: 120,
            member_stream_first_event_timeout_seconds: 30,
            member_cooldown_seconds: 60,
            member_affinity_seconds: 0,
          },
        },
      } as never
    })
    mockedPut.mockResolvedValue({ data: {} } as never)

    await savePBRFailover('model-1', [{ channel: 'channel-b', priority: 5 }])

    const [, body] = mockedPut.mock.calls[0] as [string, { config: unknown }]
    expect(body.config).toMatchObject({ member_max_attempts: 1 })
  })

  test('系统设置读取失败时回落内置默认值', async () => {
    mockedGet.mockRejectedValue(new Error('boom'))
    mockedPut.mockResolvedValue({ data: {} } as never)

    await savePBRFailover('model-1', [{ channel: 'channel-a', priority: 1 }])

    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { config: Record<string, number> },
    ]
    expect(body.config).toMatchObject({
      member_max_attempts: 2,
      member_retry_interval_seconds: 3,
      member_non_stream_response_timeout_seconds: 120,
      member_stream_first_event_timeout_seconds: 30,
      member_cooldown_seconds: 60,
      member_affinity_seconds: 0,
    })
  })
})
