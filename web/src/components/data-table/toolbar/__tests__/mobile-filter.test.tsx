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
  type ColumnFiltersState,
} from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { DataTableToolbar } from '../toolbar'

const rows = [{ name: 'Channel', model: 'model', status: 'enabled' }]
const filters = [
  {
    columnId: 'status',
    title: 'Status',
    options: [{ value: 'enabled', label: 'Enabled' }],
  },
]

function Fixture(props: { collapsibleOnMobile?: boolean }) {
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([
    { id: 'status', value: ['enabled'] },
  ])
  const table = useReactTable({
    data: rows,
    columns: [
      { accessorKey: 'name' },
      { accessorKey: 'model' },
      { accessorKey: 'status' },
    ],
    getCoreRowModel: getCoreRowModel(),
    state: { columnFilters },
    onColumnFiltersChange: setColumnFilters,
  })
  return (
    <DataTableToolbar
      table={table}
      collapsibleOnMobile={props.collapsibleOnMobile}
      searchPlaceholder='Search channels'
      searchDebounceMs={50}
      filters={filters}
      additionalSearch={
        <Input
          aria-label='Model'
          value={String(table.getColumn('model')?.getFilterValue() ?? '')}
          onChange={(event) =>
            table.getColumn('model')?.setFilterValue(event.target.value)
          }
        />
      }
      preActions={<Button>Refresh</Button>}
      viewToggle={<Button>Card view</Button>}
    />
  )
}

function setMobileViewport(mobile: boolean) {
  const original = window.matchMedia
  vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
    ...original(query),
    matches: mobile && query === '(max-width: 640px)',
  }))
}

afterEach(() => {
  vi.restoreAllMocks()
})

it('keeps the table first on mobile and opens all filters in a sheet', async () => {
  setMobileViewport(true)
  const user = userEvent.setup()
  render(<Fixture collapsibleOnMobile />)

  // The toolbar is one row: Filters + quick actions. No inline fields.
  expect(
    screen.queryByPlaceholderText('Search channels')
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('textbox', { name: 'Model' })
  ).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Filters' })).toHaveTextContent(
    'Filters'
  )
  for (const name of ['Refresh', 'Card view', 'View']) {
    expect(screen.getByRole('button', { name })).toBeVisible()
  }

  await user.click(screen.getByRole('button', { name: 'Filters' }))

  expect(screen.getByPlaceholderText('Search channels')).toBeVisible()
  expect(screen.getByRole('textbox', { name: 'Model' })).toBeVisible()
  expect(screen.getByRole('button', { name: /^Status/ })).toBeVisible()
  expect(screen.getByRole('button', { name: 'Reset' })).toBeEnabled()
  await user.click(screen.getByRole('button', { name: 'Done' }))

  expect(
    screen.queryByPlaceholderText('Search channels')
  ).not.toBeInTheDocument()
})

it('retains mobile filter values after closing, and Reset clears them', async () => {
  setMobileViewport(true)
  const user = userEvent.setup()
  render(<Fixture collapsibleOnMobile />)

  await user.click(screen.getByRole('button', { name: 'Filters' }))
  await user.type(screen.getByPlaceholderText('Search channels'), 'production')
  await user.type(screen.getByRole('textbox', { name: 'Model' }), 'gpt')
  await user.click(screen.getByRole('button', { name: 'Done' }))
  await user.click(screen.getByRole('button', { name: 'Filters' }))

  expect(screen.getByPlaceholderText('Search channels')).toHaveValue(
    'production'
  )
  expect(screen.getByRole('textbox', { name: 'Model' })).toHaveValue('gpt')
  expect(screen.getByRole('button', { name: /^Status/ })).toHaveTextContent(
    'Enabled'
  )

  await user.click(screen.getByRole('button', { name: 'Reset' }))

  expect(screen.getByPlaceholderText('Search channels')).toHaveValue('')
  expect(screen.getByRole('textbox', { name: 'Model' })).toHaveValue('')
  expect(screen.getByRole('button', { name: 'Reset' })).toBeDisabled()
})

it.each([
  { mobile: false, collapsibleOnMobile: true, condition: 'on desktop' },
  { mobile: true, collapsibleOnMobile: undefined, condition: 'without opt-in' },
])('keeps filters visible without a filter sheet $condition', (testCase) => {
  setMobileViewport(testCase.mobile)
  render(<Fixture collapsibleOnMobile={testCase.collapsibleOnMobile} />)

  expect(screen.getByPlaceholderText('Search channels')).toBeVisible()
  expect(screen.getByRole('textbox', { name: 'Model' })).toBeVisible()
  expect(screen.getByRole('button', { name: /^Status/ })).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'Filters' })
  ).not.toBeInTheDocument()
})
