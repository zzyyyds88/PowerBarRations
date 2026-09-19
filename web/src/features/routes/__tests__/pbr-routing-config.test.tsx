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
车道成员编排器（ui-spec §6.3、ADR 0006）：
- 成员候选 = 任意启用渠道的任意已声明模型，可跨渠道跨模型、无需同名；
- 同一渠道可出现多次（去重键 = (渠道, 上游真名)）；
- 无「自动添加」；默认 upstream_model 显式填所选模型名；
- 顺序即优先级；manual 需指定 active member；空成员链拦截保存。
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { savePBRFailover } from '../api'
import { LaneComposer } from '../components/lane-composer'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), put: vi.fn(), delete: vi.fn(), post: vi.fn() },
}))

const mockedGet = vi.mocked(api.get)
const mockedPut = vi.mocked(api.put)

/** 两个渠道：channel-a 声明两个模型，channel-b 声明一个。 */
function mockCatalog() {
  mockedGet.mockImplementation(async (url: string) => {
    if (url === '/api/v1/channels') {
      return {
        data: {
          items: [
            {
              name: 'channel-a',
              enabled: true,
              models: ['a-model-1', 'a-model-2'],
              model_mapping: {},
            },
            {
              name: 'channel-b',
              enabled: true,
              models: ['b-model-1'],
              model_mapping: { 'pool-fast': 'vendor-b/model-1' },
            },
          ],
        },
      } as never
    }
    if (url === '/api/v1/system/options') {
      return { data: { lane_defaults: {} } } as never
    }
    throw new Error(`Unexpected GET ${url}`)
  })
}

function renderComposer(
  props: Partial<Parameters<typeof LaneComposer>[0]> = {}
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <LaneComposer onSaved={() => {}} onCancel={() => {}} {...props} />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  mockedPut.mockResolvedValue({ data: {} } as never)
})

afterEach(() => {
  cleanup()
})

describe('车道成员编排器', () => {
  test('可为任意路由键跨渠道挑选成员并保存', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderComposer()

    await user.type(screen.getByLabelText('Route key'), 'pool-fast')
    // 展开两个渠道并各选一个模型（无需声明该路由键）。
    await user.click(await screen.findByRole('button', { name: /channel-a/ }))
    await user.click(await screen.findByRole('button', { name: /a-model-1/ }))
    await user.click(screen.getByRole('button', { name: /channel-b/ }))
    await user.click(await screen.findByRole('button', { name: /b-model-1/ }))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
    const [url, body] = mockedPut.mock.calls[0] as [
      string,
      { members: { channel: string; upstream_model: string }[] },
    ]
    expect(url).toBe('/api/v1/lanes/pool-fast')
    expect(body.members.map((m) => [m.channel, m.upstream_model])).toEqual([
      ['channel-a', 'a-model-1'],
      ['channel-b', 'b-model-1'],
    ])
  })

  test('同一渠道的两个模型可作为两个成员存在', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderComposer({ model: 'pool-fast' })

    await user.click(await screen.findByRole('button', { name: /channel-a/ }))
    await user.click(await screen.findByRole('button', { name: /a-model-1/ }))
    await user.click(screen.getByRole('button', { name: /a-model-2/ }))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { members: { channel: string; upstream_model: string }[] },
    ]
    expect(body.members.map((m) => m.upstream_model)).toEqual([
      'a-model-1',
      'a-model-2',
    ])
    expect(body.members.every((m) => m.channel === 'channel-a')).toBe(true)
  })

  test('已加入的成员在左栏显示为选中且不可重复加入', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderComposer()
    await user.type(screen.getByLabelText('Route key'), 'pool-fast')

    await user.click(await screen.findByRole('button', { name: /channel-a/ }))
    await user.click(await screen.findByRole('button', { name: /a-model-1/ }))
    const item = await screen.findByRole('button', { name: /a-model-1/ })
    expect(item).toBeDisabled()
  })

  test('排序与改名随保存提交，顺序即优先级', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderComposer({ model: 'pool-fast' })

    await user.click(await screen.findByRole('button', { name: /channel-a/ }))
    await user.click(await screen.findByRole('button', { name: /a-model-1/ }))
    await user.click(screen.getByRole('button', { name: /a-model-2/ }))
    // 把第二个成员上移并改名（两个成员同渠道，label 相同，取排序后的首位）。
    await user.click(screen.getAllByRole('button', { name: 'Move up' })[1])
    const rename = screen.getAllByLabelText('Upstream model for channel-a')[0]
    await user.clear(rename)
    await user.type(rename, 'renamed-a2')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { members: { upstream_model: string; priority: number }[] },
    ]
    expect(body.members[0].upstream_model).toBe('renamed-a2')
    expect(body.members[0].priority).toBeGreaterThan(body.members[1].priority)
  })

  test('空成员链阻止保存', async () => {
    mockCatalog()
    renderComposer()
    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Route key'), 'pool-fast')

    expect(await screen.findByText('No members yet')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(mockedPut).not.toHaveBeenCalled()
  })

  test('manual 模式必须指定 active member 才能保存', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderComposer({ model: 'pool-fast' })

    await user.click(await screen.findByRole('button', { name: /channel-a/ }))
    await user.click(await screen.findByRole('button', { name: /a-model-1/ }))
    await user.click(screen.getByRole('combobox', { name: 'Mode' }))
    await user.click(await screen.findByRole('option', { name: 'manual' }))

    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    await user.click(screen.getByRole('combobox', { name: 'Active member' }))
    await user.click(await screen.findByRole('option', { name: /channel-a/ }))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { mode: string; active_member: string },
    ]
    expect(body.mode).toBe('manual')
    expect(body.active_member).toBe('channel-a/a-model-1')
  })

  test('不渲染「自动添加」入口（成员全部人工挑选）', async () => {
    mockCatalog()
    renderComposer()
    await screen.findByRole('button', { name: /channel-a/ })
    expect(
      screen.queryByRole('button', { name: /auto.?add/i })
    ).not.toBeInTheDocument()
  })
})

describe('savePBRFailover 六键来源', () => {
  test('调用方显式传入的六键优先', async () => {
    mockedGet.mockRejectedValue(new Error('not found'))
    await savePBRFailover('model-1', [{ channel: 'channel-a', priority: 10 }], {
      config: { member_max_attempts: 9 },
    })
    const [, body] = mockedPut.mock.calls[0] as [string, { config: unknown }]
    expect(body.config).toMatchObject({ member_max_attempts: 9 })
  })

  test('系统设置读取失败时回落内置默认值', async () => {
    mockedGet.mockRejectedValue(new Error('boom'))
    await savePBRFailover('model-1', [{ channel: 'channel-a', priority: 1 }])
    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { config: Record<string, number> },
    ]
    expect(body.config).toMatchObject({
      member_max_attempts: 2,
      member_retry_interval_seconds: 3,
      member_cooldown_seconds: 60,
      member_affinity_seconds: 0,
    })
  })

  test('已有车道保留其自身六键，只改成员顺序', async () => {
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/system/options') {
        return { data: { lane_defaults: { member_max_attempts: 5 } } } as never
      }
      return {
        data: { config: { member_max_attempts: 1 } },
      } as never
    })
    await savePBRFailover('model-1', [{ channel: 'channel-b', priority: 5 }])
    const [, body] = mockedPut.mock.calls[0] as [string, { config: unknown }]
    expect(body.config).toMatchObject({ member_max_attempts: 1 })
  })
})
