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
import type { ColumnDef } from '@tanstack/react-table'
import { render, screen, cleanup } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'

import { CHANNEL_PROTOCOL_FILTER_OPTIONS, type TagRow } from '../../lib'
import { channelSchema, type Channel } from '../../types'
import { ProtocolCell, useChannelsColumns } from '../channels-columns'
import { ChannelsProvider } from '../channels-provider'

// 渠道列表的「协议」列与工具栏协议筛选（ui-spec §6.4）。契约口径：
// 该列只能取 4 种上游协议，旧厂商类型落「自定义」桶且不被静默改写；
// 厂商 type 不作为用户面维度；标签与编辑弹窗共用 getChannelProtocol 一处实现。

function makeChannel(overrides: Partial<Channel> = {}): Channel {
  return channelSchema.parse({
    id: 1,
    name: 'upstream-a',
    type: 1,
    key: '',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    models: 'gpt-4o',
    base_url: 'https://upstream.example',
    ...overrides,
  })
}

function columnIdentifier(column: ColumnDef<Channel>): string {
  if ('id' in column && typeof column.id === 'string') {
    return column.id
  }
  if ('accessorKey' in column && typeof column.accessorKey === 'string') {
    return column.accessorKey
  }
  return ''
}

function ColumnIdsProbe() {
  const columns = useChannelsColumns()
  return (
    <p data-testid='column-ids'>{columns.map(columnIdentifier).join(',')}</p>
  )
}

function renderColumnIds() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <ColumnIdsProbe />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  return screen.getByTestId('column-ids').textContent ?? ''
}

afterEach(() => {
  cleanup()
})

it('labels every adapter configuration with exactly one of the four upstream protocols', () => {
  const cases: Array<{ channel: Channel; label: string }> = [
    { channel: makeChannel({ type: 1 }), label: 'OpenAI compatible' },
    {
      channel: makeChannel({
        type: 1,
        settings: JSON.stringify({ protocol: 'openai-responses' }),
      }),
      label: 'OpenAI Responses',
    },
    { channel: makeChannel({ type: 14 }), label: 'Anthropic' },
    { channel: makeChannel({ type: 24 }), label: 'Gemini' },
  ]
  const allLabels = cases.map((testCase) => testCase.label)

  for (const testCase of cases) {
    cleanup()
    render(<ProtocolCell channel={testCase.channel} />)
    // 协议图标是装饰性 SVG，会附带同名 <title>（不可见节点）；只断言真正承载文本的元素。
    const labeled = screen
      .getAllByText(testCase.label)
      .filter((element) => element.tagName.toUpperCase() !== 'TITLE')
    expect(labeled).toHaveLength(1)
    expect(labeled[0]).toBeVisible()
    for (const other of allLabels.filter((label) => label !== testCase.label)) {
      expect(screen.queryByText(other)).not.toBeInTheDocument()
    }
    expect(screen.queryByText('Custom')).not.toBeInTheDocument()
  }
})

it('shows Custom for legacy channels whose adapter type is outside the four protocols', () => {
  render(<ProtocolCell channel={makeChannel({ type: 3 })} />)
  expect(screen.getAllByText('Custom')[0]).toBeVisible()
})

it('keeps the channel level protocol ahead of the adapter type so list and editor agree', () => {
  render(
    <ProtocolCell
      channel={makeChannel({
        type: 14,
        settings: JSON.stringify({ protocol: 'openai-chat' }),
      })}
    />
  )
  expect(screen.getAllByText('OpenAI compatible')[0]).toBeVisible()
  expect(screen.queryByText('Anthropic')).not.toBeInTheDocument()
})

it('renders a dash for tag aggregate rows instead of claiming one protocol', () => {
  const tagRow: TagRow = {
    ...makeChannel({ name: 'prod', id: 0, type: 0 }),
    children: [makeChannel({ type: 24 })],
  }
  render(<ProtocolCell channel={tagRow} />)
  expect(screen.getByText('-')).toBeVisible()
  expect(screen.queryByText('Custom')).not.toBeInTheDocument()
})

it('offers exactly the four protocols plus custom as toolbar filter options', () => {
  expect(CHANNEL_PROTOCOL_FILTER_OPTIONS.map((option) => option.value)).toEqual(
    ['openai-chat', 'openai-responses', 'anthropic', 'gemini', 'custom']
  )
  // 窄列与筛选面板用短名：端点路径只属于编辑弹窗，出现在这里只会溢出成噪音。
  for (const option of CHANNEL_PROTOCOL_FILTER_OPTIONS) {
    expect(option.label).not.toContain('/v1')
  }
})

it('adds a protocol column and drops the id column from the table', () => {
  const ids = renderColumnIds()
  const columns = ids.split(',')
  expect(columns).toContain('protocol')
  expect(columns).not.toContain('id')
  expect(columns).toContain('name')
})
