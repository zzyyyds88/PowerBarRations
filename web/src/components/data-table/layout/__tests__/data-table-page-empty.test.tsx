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
import { expect, it, vi } from 'vitest'

import { DataTablePage } from '@/components/data-table'

interface Row {
  id: number
}

const columns: ColumnDef<Row>[] = [{ accessorKey: 'id', header: 'ID' }]

function EmptyHarness() {
  const table = useReactTable({
    data: [],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      emptyTitle='No Rows Found'
      emptyDescription='Nothing matched.'
      emptyAction={<button type='button'>Clear filters</button>}
      paginationInFooter={false}
    />
  )
}

it('renders the empty-state action in the mobile card list too', () => {
  const matchMedia = window.matchMedia
  vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
    ...matchMedia(query),
    matches: query.includes('max-width'),
  }))

  render(<EmptyHarness />)

  expect(screen.getByText('No Rows Found')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Clear filters' })).toBeVisible()
})
