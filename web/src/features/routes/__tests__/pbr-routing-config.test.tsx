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
车道成员编排器（ui-spec §6.3、ADR 0008）：
- 成员候选 = 任意启用渠道的任意已声明模型，可跨渠道跨模型、无需同名；
- 去重键 = **(渠道, 所选模型)**：同一渠道的不同模型可多次出现，同一模型不可重复；
- 成员只存所选模型，上游真名由渠道映射派生，**成员级改名输入框已移除**；
- 无「自动添加」；顺序即优先级；manual 需指定 active member；空成员链拦截保存。
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { savePBRFailover } from '../api'
import { LaneComposer } from '../components/lane-composer'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), put: vi.fn(), delete: vi.fn(), post: vi.fn() },
}))

const mockedGet = vi.mocked(api.get)
const mockedPut = vi.mocked(api.put)

/**
 * 两个渠道：channel-a 声明两个模型，channel-b 声明一个。
 * channel-b 的映射键是它声明的模型名（b-model-1 → vendor-b/real-1），
 * 用于验证"以成员所选模型为键"查映射（池化车道也能命中）。
 */
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
              model_mapping: { 'b-model-1': 'vendor-b/real-1' },
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
      { members: { channel: string; model: string }[] },
    ]
    expect(url).toBe('/api/v1/lanes/pool-fast')
    // 成员只存所选模型：发出去的是用户点的模型名本身，不是渠道映射右值。
    expect(body.members.map((m) => [m.channel, m.model])).toEqual([
      ['channel-a', 'a-model-1'],
      ['channel-b', 'b-model-1'],
    ])
    // 上游真名是服务端派生值，写载荷里**不得**出现（ADR 0008）。
    expect(body.members.some((m) => 'upstream_model' in (m as object))).toBe(
      false
    )
  })

  test('池化车道按成员所选模型命中渠道映射（查表键不是车道名）', async () => {
    mockCatalog()
    const user = userEvent.setup()
    // 车道名 111 与映射键 b-model-1 不同；映射仍须生效（ADR 0008）。
    renderComposer({ model: '111' })

    await user.click(await screen.findByRole('button', { name: /channel-b/ }))
    await user.click(await screen.findByRole('button', { name: /b-model-1/ }))
    // 成员行只读展示派生出的上游真名（渠道映射 b-model-1 → vendor-b/real-1）。
    expect(
      await screen.findByText(/Resolved upstream.*vendor-b\/real-1/)
    ).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { members: { channel: string; model: string }[] },
    ]
    expect(body.members).toEqual([
      expect.objectContaining({ channel: 'channel-b', model: 'b-model-1' }),
    ])
  })

  test('解析后的上游是渲染期派生：同一模型在不同渠道映射下显示不同', async () => {
    // 锁定"渲染期派生"而非"加入时算一次"（ADR 0008）：成员行显示的真名来自
    // 当前渠道目录，所以渠道映射一变，编辑器无需重开就能看到新真名。
    // 这里用两个渠道声明同一个模型名、映射到不同真名来验证。
    mockedGet.mockImplementation(async (url: string) => {
      if (url === '/api/v1/channels') {
        return {
          data: {
            items: [
              {
                name: 'ch-x',
                enabled: true,
                models: ['shared-model'],
                model_mapping: { 'shared-model': 'vendor-x/real' },
              },
              {
                name: 'ch-y',
                enabled: true,
                models: ['shared-model'],
                model_mapping: { 'shared-model': 'vendor-y/real' },
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
    const user = userEvent.setup()
    renderComposer({ model: 'pool' })

    // 两个渠道都声明 shared-model，所以左栏有两个同名按钮——按渠道展开后再取。
    // 展开 ch-x，点它的 shared-model。
    await user.click(await screen.findByRole('button', { name: /ch-x/ }))
    await user.click(
      (await screen.findAllByRole('button', { name: /shared-model/ }))[0]
    )
    expect(
      await screen.findByText(/Resolved upstream.*vendor-x\/real/)
    ).toBeVisible()

    // 展开 ch-y，点它自己的 shared-model → 按该渠道映射派生，与上一条互不干扰。
    await user.click(screen.getByRole('button', { name: /ch-y/ }))
    const yButtons = await screen.findAllByRole('button', {
      name: /shared-model/,
    })
    // 未被加入的那个（ch-y 的）仍可点。
    const enabled = yButtons.find((b) => !(b as HTMLButtonElement).disabled)
    expect(enabled).toBeDefined()
    await user.click(enabled as HTMLElement)
    expect(
      await screen.findByText(/Resolved upstream.*vendor-y\/real/)
    ).toBeVisible()
    // 两条都在：同一模型名 + 不同渠道 = 两个不同成员（唯一键含渠道）。
    expect(screen.getByText(/vendor-x\/real/)).toBeVisible()
    expect(screen.getByText(/vendor-y\/real/)).toBeVisible()
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
      { members: { channel: string; model: string }[] },
    ]
    expect(body.members.map((m) => m.model)).toEqual(['a-model-1', 'a-model-2'])
    expect(body.members.every((m) => m.channel === 'channel-a')).toBe(true)
  })

  test('左栏按 (渠道, 模型) 去重：已加入的模型禁用，同渠道别的模型仍可加', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderComposer()
    await user.type(screen.getByLabelText('Route key'), 'pool-fast')

    await user.click(await screen.findByRole('button', { name: /channel-a/ }))
    await user.click(await screen.findByRole('button', { name: /a-model-1/ }))
    const added = await screen.findByRole('button', { name: /a-model-1/ })
    expect(added).toBeDisabled()
    // 同渠道的**不同**模型不受影响（去重键是 (渠道, 所选模型)，不是渠道）。
    expect(screen.getByRole('button', { name: /a-model-2/ })).toBeEnabled()
  })

  test('排序随保存提交，顺序即优先级', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderComposer({ model: 'pool-fast' })

    await user.click(await screen.findByRole('button', { name: /channel-a/ }))
    await user.click(await screen.findByRole('button', { name: /a-model-1/ }))
    await user.click(screen.getByRole('button', { name: /a-model-2/ }))
    // 把第二个成员上移（两个成员同渠道，label 相同，取排序后的首位）。
    await user.click(screen.getAllByRole('button', { name: 'Move up' })[1])
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { members: { model: string; priority: number }[] },
    ]
    expect(body.members[0].model).toBe('a-model-2')
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

  test('成员所选模型未被渠道声明时，成员行内联告警（不拦截保存）', async () => {
    mockCatalog()
    const user = userEvent.setup()
    // 车道名 hermes-lane 与所选模型无关：告警判据只看所选模型是否在该渠道声明范围内。
    renderComposer({
      model: 'hermes-lane',
      initialMembers: [
        {
          id: 'draft-1',
          channel: 'channel-a',
          model: 'undeclared-model',
          enabled: true,
        },
      ],
    })

    expect(await screen.findByText(/does not declare/)).toBeVisible()
    // 告警不拦截保存。
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
  })

  test('成员所选模型已被渠道声明时不告警', async () => {
    mockCatalog()
    renderComposer({
      model: 'hermes-lane',
      initialMembers: [
        {
          id: 'draft-1',
          channel: 'channel-a',
          model: 'a-model-1',
          enabled: true,
        },
      ],
    })

    expect(await screen.findAllByText('a-model-1')).not.toHaveLength(0)
    expect(screen.queryByText(/does not declare/)).not.toBeInTheDocument()
  })

  // 回归：Base UI 的 SelectValue 默认回显原始值（failover/manual），会绕过 i18n，
  // 中文界面出现英文。触发框必须渲染翻译后的标签。
  test('模式下拉触发框显示翻译后的标签而非原始值', async () => {
    // 测试环境的 i18n 资源为空（t 回退成 key），无法区分"原始值"与"翻译值"；
    // 这里临时注入与原始值不同的标签，再断言触发框显示的是标签。
    i18next.addResourceBundle('en', 'translation', {
      failover: 'FAILOVER-LABEL',
      manual: 'MANUAL-LABEL',
    })
    try {
      mockCatalog()
      const user = userEvent.setup()
      renderComposer({ model: 'pool-fast' })

      const trigger = screen.getByRole('combobox', { name: 'Mode' })
      expect(trigger).toHaveTextContent('FAILOVER-LABEL')
      expect(trigger).not.toHaveTextContent(/^failover$/)

      await user.click(trigger)
      await user.click(
        await screen.findByRole('option', { name: 'MANUAL-LABEL' })
      )
      expect(screen.getByRole('combobox', { name: 'Mode' })).toHaveTextContent(
        'MANUAL-LABEL'
      )
    } finally {
      i18next.removeResourceBundle('en', 'translation')
    }
  })
})

describe('savePBRFailover 六键来源', () => {
  test('调用方显式传入的六键优先', async () => {
    mockedGet.mockRejectedValue(new Error('not found'))
    await savePBRFailover(
      'model-1',
      [{ channel: 'channel-a', model: 'model-1', priority: 10 }],
      {
        config: { member_max_attempts: 9 },
      }
    )
    const [, body] = mockedPut.mock.calls[0] as [string, { config: unknown }]
    expect(body.config).toMatchObject({ member_max_attempts: 9 })
  })

  test('系统设置读取失败时回落内置默认值', async () => {
    mockedGet.mockRejectedValue(new Error('boom'))
    await savePBRFailover('model-1', [
      { channel: 'channel-a', model: 'model-1', priority: 1 },
    ])
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
    await savePBRFailover('model-1', [
      { channel: 'channel-b', model: 'model-1', priority: 5 },
    ])
    const [, body] = mockedPut.mock.calls[0] as [string, { config: unknown }]
    expect(body.config).toMatchObject({ member_max_attempts: 1 })
  })
})

// api-spec §4.2 通则（读写闭环，强制）：成员写入是全量替换，草稿里读到的每个
// 成员级字段都必须在保存时原样带回。本仓已因此丢过 overrides / public_alias，
// 这组用例把"再丢一次"变成测试失败。**`upstream_model` 例外**：它是服务端由渠道
// 映射推导的只读派生值（ADR 0008），只展示、不回传。
describe('成员级字段读回写闭环', () => {
  test('保存时带回 model / public_alias / overrides / enabled，不发 upstream_model', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderComposer({
      model: 'model-1',
      initialMembers: [
        {
          id: 'draft-1',
          channel: 'channel-b',
          model: 'b-model-1',
          publicAlias: 'fast-a',
          overrides: { member_cooldown_seconds: 120 },
          enabled: false,
        },
      ],
    })

    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(mockedPut).toHaveBeenCalled())

    const [, body] = mockedPut.mock.calls[0] as [
      string,
      {
        members: {
          channel: string
          model: string
          public_alias: string
          priority: number
          enabled: boolean
          overrides: Record<string, number>
        }[]
      },
    ]
    expect(body.members).toHaveLength(1)
    const saved = body.members[0]
    expect(saved.channel).toBe('channel-b')
    // 写回的是**所选模型**（草稿字段），不是展示用的派生真名 vendor-b/real-1。
    expect(saved.model).toBe('b-model-1')
    // 这三条就是此前被静默清空的字段。
    expect(saved.public_alias).toBe('fast-a')
    expect(saved.overrides).toEqual({ member_cooldown_seconds: 120 })
    expect(saved.enabled).toBe(false)
    // 派生只读值不进写载荷。
    expect('upstream_model' in saved).toBe(false)
  })

  test('点击开关把成员置为停用并随保存回传', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderComposer({
      model: 'model-1',
      initialMembers: [
        {
          id: 'draft-1',
          channel: 'channel-a',
          model: 'a-model-1',
          enabled: true,
        },
      ],
    })

    // 行内开关的可访问名带渠道 + 所选模型（ui-spec §6.3）。
    await user.click(
      screen.getByRole('switch', {
        name: 'Member enabled for channel-a · a-model-1',
      })
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(mockedPut).toHaveBeenCalled())

    const [, body] = mockedPut.mock.calls[0] as [
      string,
      { members: { enabled: boolean; public_alias: string }[] },
    ]
    expect(body.members[0].enabled).toBe(false)
    // 无别名的成员也必须显式回传空串（缺席 = 被整体替换清空）。
    expect(body.members[0].public_alias).toBe('')
  })

  test('停用成员后仍能保存（合法人工态，不得被拦截）', async () => {
    mockCatalog()
    const user = userEvent.setup()
    renderComposer({
      model: 'model-1',
      initialMembers: [
        {
          id: 'draft-1',
          channel: 'channel-a',
          model: 'a-model-1',
          enabled: false,
        },
      ],
    })

    const saveButton = screen.getByRole('button', { name: 'Save' })
    expect(saveButton).toBeEnabled()
    await user.click(saveButton)
    await waitFor(() => expect(mockedPut).toHaveBeenCalled())
  })

  // ui-spec §6.3：上游真名只读派生，成员行显示「解析后的上游」；成员级改名输入框
  // 已整体移除（ADR 0008）。派生规则本身由 lane-member-upstream 的纯函数单测守卫。
  test('成员行只读展示解析后的上游，不再有改名输入框', async () => {
    mockCatalog()
    renderComposer({
      model: 'pooled-lane',
      initialMembers: [
        {
          id: 'draft-1',
          channel: 'channel-b',
          model: 'b-model-1',
          enabled: true,
        },
      ],
    })

    expect(
      await screen.findByText(/Resolved upstream.*vendor-b\/real-1/)
    ).toBeVisible()
    // 成员身份仍显示所选模型（不是派生真名）。
    expect(screen.getByText('b-model-1')).toBeVisible()
    expect(
      screen.queryByLabelText('Upstream model for channel-b')
    ).not.toBeInTheDocument()
  })
})
