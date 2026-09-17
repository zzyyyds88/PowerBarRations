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
import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'

import { DataTablePage } from '@/components/data-table'

interface Row {
  id: number
}

const columns: ColumnDef<Row>[] = [{ accessorKey: 'id', header: 'ID' }]

function ErrorHarness(props: { onRetry: () => void }) {
  const table = useReactTable({
    data: [],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isError
      error={new Error('Backend exploded')}
      onRetry={props.onRetry}
      emptyTitle='No Rows Found'
      emptyDescription='Create your first row to get started.'
      paginationInFooter={false}
    />
  )
}

it('renders the retryable error state instead of the empty state on failure', () => {
  const onRetry = vi.fn()
  render(<ErrorHarness onRetry={onRetry} />)

  expect(screen.getByText('Backend exploded')).toBeVisible()
  expect(screen.queryByText('No Rows Found')).not.toBeInTheDocument()
  expect(
    screen.queryByText('Create your first row to get started.')
  ).not.toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(onRetry).toHaveBeenCalledTimes(1)
})
