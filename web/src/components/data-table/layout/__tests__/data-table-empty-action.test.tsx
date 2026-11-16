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
import {
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'

import {
  DataTableEmptyActionProvider,
  DataTablePage,
} from '@/components/data-table'

interface Row {
  id: number
}

const columns: ColumnDef<Row>[] = [{ accessorKey: 'id', header: 'ID' }]

function EmptyHarness(props: { explicitAction?: boolean }) {
  const table = useReactTable({
    data: [],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  const page = (
    <DataTablePage
      table={table}
      columns={columns}
      emptyTitle='No Rows Found'
      emptyDescription='Nothing matched.'
      emptyAction={
        props.explicitAction ? (
          <button type='button'>Explicit action</button>
        ) : undefined
      }
      paginationInFooter={false}
    />
  )

  return (
    <DataTableEmptyActionProvider
      action={<button type='button'>Create from page</button>}
    >
      {page}
    </DataTableEmptyActionProvider>
  )
}

it('uses the page-level empty action when the table has none', () => {
  render(<EmptyHarness />)

  expect(
    screen.getByRole('button', { name: 'Create from page' })
  ).toBeInTheDocument()
})

it('keeps an explicit emptyAction over the page-level default', () => {
  render(<EmptyHarness explicitAction />)

  expect(
    screen.getByRole('button', { name: 'Explicit action' })
  ).toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Create from page' })
  ).not.toBeInTheDocument()
})
