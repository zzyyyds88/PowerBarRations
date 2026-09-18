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
import { afterAll, afterEach, beforeEach, expect, test, vi } from 'vitest'

import en from '@/i18n/locales/en.json'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { useCommonLogsColumns } from '../columns/common-logs-columns'

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

function DetailPreview(props: {
  other: LogOtherData
  isAdmin: boolean
  type?: number
}) {
  const table = useReactTable({
    data: [makeLog(props.other, props.type ?? 2)],
    columns: useCommonLogsColumns(props.isAdmin, false),
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

function renderPreview(other: LogOtherData, isAdmin = true, type = 2) {
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <DetailPreview other={other} isAdmin={isAdmin} type={type} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return screen.getByRole('button')
}

// PBR 通用日志只存元数据：详情预览只展示排障相关的状态/原因，
// 不渲染上游的计费/额度/分组/倍率文案。
test.each([
  {
    name: 'stream failure shows the end reason',
    other: {
      stream_status: { status: 'error', end_reason: 'upstream closed' },
    },
    expected: 'Stream Status: upstream closed',
  },
  {
    name: 'system prompt override is flagged',
    other: { is_system_prompt_overwritten: true },
    expected: 'System Prompt Override',
  },
  {
    name: 'missing details fall back to the content placeholder',
    other: {},
    expected: '—',
  },
])('$name', ({ other, expected }) => {
  const preview = renderPreview(other)
  expect(preview.textContent).toBe(expected)
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

test('quota saturation is first for admins', () => {
  const preview = renderPreview(saturationOther, true)
  expect(preview.textContent).toBe('Quota clamped')
})

test('quota saturation never leaks to non-admins', () => {
  const preview = renderPreview(saturationOther, false)
  expect(preview.textContent).toBe('—')
})

test('refund logs preview their recorded reason', () => {
  const preview = renderPreview({ reason: 'upstream timeout' }, false, 6)
  expect(preview.textContent).toBe('upstream timeout')
})

test('billing and quota wording never appears in the details preview', () => {
  renderPreview(
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
  const preview = screen.getByRole('button')
  expect(preview.textContent).toBe('—')
  expect(preview.textContent).not.toMatch(/Per-call|Standard|Group Ratio|\$/i)
})
