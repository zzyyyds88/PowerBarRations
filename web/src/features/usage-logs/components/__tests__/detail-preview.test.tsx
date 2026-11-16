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
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import {
  afterAll,
  afterEach,
  beforeEach,
  describe,
  expect,
  test,
  vi,
} from 'vitest'

import en from '@/i18n/locales/en.json'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { useCommonLogsColumns } from '../columns/common-logs-columns'
import { DetailsDialog } from '../dialogs/details-dialog'

vi.mock('@lobehub/icons', () => ({}))
vi.hoisted(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
})
afterAll(() => vi.unstubAllGlobals())

function makeLog(other: LogOtherData, type = 2): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1,
    type,
    content: '',
    username: 'user',
    token_name: 'token',
    model_name: 'gpt-test',
    quota: 5000,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 0,
    is_stream: false,
    channel: 1,
    channel_name: '',
    token_id: 1,
    group: 'default',
    ip: '',
    other: JSON.stringify(other),
    request_id: 'req-1',
    upstream_request_id: '',
  }
}

// 只渲染 content（Details）列的单元格，验证它的列内契约。
function DetailPreview(props: { other: LogOtherData; type?: number }) {
  const table = useReactTable({
    data: [makeLog(props.other, props.type ?? 2)],
    columns: useCommonLogsColumns(true, false),
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0].getAllCells()
    .find((item) => item.column.id === 'content')
  if (!cell) throw new Error('The log must have a content column')
  return flexRender(cell.column.columnDef.cell, cell.getContext())
}

let client: QueryClient
const i18n = createInstance()
beforeEach(async () => {
  await i18n.init({
    lng: 'en',
    resources: { en },
    interpolation: { escapeValue: false },
  })
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(() => {
  client.clear()
})

function renderPreview(other: LogOtherData, type = 2) {
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <DetailPreview other={other} type={type} />
      </QueryClientProvider>
    </I18nextProvider>
  )
}

// PBR 日志详情的权威入口是详情行展开与详情弹窗（ui-spec §6.6）；
// stream status / 额度饱和 / 退款原因在弹窗断言（见本文件后半）。

describe('details column contract', () => {
  // PBR 日志（other.pbr 存在）渲染展开按钮。
  test('PBR logs render an expand button in the details column', () => {
    renderPreview({
      pbr: {
        lane: 'lane-a',
        route_source: 'explicit',
        upstream_model: 'upstream-model',
        error_kind: '',
        error_summary: '',
        http_status: 200,
        total_ms: 120,
        estimated_cost: 0,
        attempts: [],
        total_attempts: 1,
        inbound_format: 'chat_completions',
        success: true,
      },
    })
    expect(screen.getByRole('button', { name: 'Expand' })).toBeVisible()
  })

  // 非 PBR 日志渲染占位符，不渲染按钮。
  test('non-PBR logs render the placeholder without a button', () => {
    renderPreview({})
    expect(screen.queryByRole('button')).toBeNull()
    expect(screen.getByText('—')).toBeVisible()
  })

  // 计费/额度文案绝不出现在详情列（PBR 无额度语义）。
  test('billing and quota wording never appears in the details column', () => {
    renderPreview({
      model_ratio: 1,
      completion_ratio: 2,
      group_ratio: 1,
      model_price: 0.25,
      billing_mode: 'tiered_expr',
      fixed_price: 0.04,
      matched_tier: 'image',
    })
    expect(screen.getByText('—')).toBeVisible()
    expect(screen.queryByText(/Per-call|Standard|Group Ratio|\$/i)).toBeNull()
  })
})

const saturationOther: LogOtherData = {
  admin_info: {
    quota_saturation: {
      op: 'round',
      kind: 'overflow',
      original: 3e9,
      clamped: 2147483647,
    },
  },
}

describe('moved previews live in the details dialog', () => {
  const queryClients: QueryClient[] = []

  afterEach(() => {
    for (const client of queryClients) client.clear()
    queryClients.length = 0
  })

  function renderDialog(other: LogOtherData, isAdmin: boolean, type = 2) {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClients.push(queryClient)
    render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={queryClient}>
          <DetailsDialog
            log={makeLog(other, type)}
            isAdmin={isAdmin}
            isRoot={false}
            open
            onOpenChange={() => undefined}
          />
        </QueryClientProvider>
      </I18nextProvider>
    )
  }

  // 额度饱和徽标只给管理员（不泄漏给 self view）。
  test('quota saturation is first for admins', () => {
    renderDialog(saturationOther, true)
    expect(screen.getByText('Quota clamped')).toBeVisible()
  })

  test('quota saturation never leaks to non-admins', () => {
    renderDialog(saturationOther, false)
    expect(screen.queryByText('Quota clamped')).toBeNull()
  })

  // 退款原因在弹窗展示。
  test('refund logs preview their recorded reason', () => {
    renderDialog({ reason: 'upstream timeout' }, false, 6)
    expect(screen.getByText('upstream timeout')).toBeVisible()
  })

  // 系统提示覆盖标记在弹窗展示（System Prompt 行 + Overwritten 徽章）。
  test('system prompt override is flagged in the details dialog', () => {
    renderDialog({ is_system_prompt_overwritten: true }, true)
    expect(screen.getByText('System Prompt')).toBeVisible()
    expect(screen.getByText('Overwritten')).toBeVisible()
  })

  // 弹窗不渲染计费/额度文案。
  test('billing and quota wording never appears in the details dialog', () => {
    renderDialog(
      {
        model_ratio: 1,
        completion_ratio: 2,
        group_ratio: 1,
        model_price: 0.25,
        billing_mode: 'tiered_expr',
        fixed_price: 0.04,
        matched_tier: 'image',
      },
      true
    )
    expect(screen.queryByText(/Per-call|Standard|Group Ratio|\$/i)).toBeNull()
  })
})
