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
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { useMemo, useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { Toaster } from 'sonner'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { apiKeySchema, type ApiKey } from '../../types'
import { ApiKeysMultiDeleteDialog } from '../api-keys-multi-delete-dialog'
import { ApiKeysProvider } from '../api-keys-provider'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

const keyA = apiKeySchema.parse({
  id: 1,
  name: 'alpha',
  key: 'pbr-alpha',
  status: 1,
  cost: 0,
  expired_time: -1,
  created_time: 0,
  accessed_time: 0,
  model_limits_enabled: false,
})
const keyB = apiKeySchema.parse({
  ...keyA,
  id: 2,
  name: 'beta',
  key: 'pbr-beta',
})

function Fixture(props: { onOpenChange: (open: boolean) => void }) {
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({
    '1': true,
    '2': true,
  })
  const columns = useMemo<ColumnDef<ApiKey>[]>(
    () => [
      {
        id: 'name',
        accessorKey: 'name',
        header: 'Name',
        cell: () => null,
      },
    ],
    []
  )
  const table = useReactTable({
    columns,
    data: [keyA, keyB],
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) => String(row.id),
    state: { rowSelection },
    onRowSelectionChange: setRowSelection,
  })
  return (
    <ApiKeysMultiDeleteDialog
      open
      onOpenChange={props.onOpenChange}
      table={table}
    />
  )
}

afterEach(() => {
  cleanup()
})

it('reports partial failures, keeps failed rows selected and closes only when all succeed', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      items: [
        { id: 1, name: 'alpha' },
        { id: 2, name: 'beta' },
      ],
    },
  })
  const del = vi
    .spyOn(api, 'delete')
    .mockImplementation(async (url: string) => {
      if (url === '/api/keys/beta') throw new Error('beta locked')
      return { data: {} }
    })
  const onOpenChange = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <ApiKeysProvider>
          <Fixture onOpenChange={onOpenChange} />
          <Toaster />
        </ApiKeysProvider>
      </QueryClientProvider>
    </I18nextProvider>
  )

  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Delete' }))

  await waitFor(() => expect(del).toHaveBeenCalledTimes(2))
  // 部分失败：对话框保留，选择收缩为失败项，标题计数随之变为 1。
  await waitFor(() =>
    expect(screen.getByText('Delete 1 API key(s)?')).toBeVisible()
  )
  expect(onOpenChange).not.toHaveBeenCalledWith(false)

  // 重试剩余失败项成功后关闭对话框。
  del.mockResolvedValue({ data: {} })
  await user.click(screen.getByRole('button', { name: 'Delete' }))
  await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
})
