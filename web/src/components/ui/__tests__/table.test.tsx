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
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../table'

function Fixture(props: { wrap?: boolean; withContainer?: boolean }) {
  return (
    <Table wrap={props.wrap} withContainer={props.withContainer}>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow>
          <TableCell>pbr-TKDDPzappgFHB01UEZQAdqku7QAURCSJ</TableCell>
        </TableRow>
      </TableBody>
    </Table>
  )
}

describe('Table wrap switch', () => {
  it('leaves cells on the nowrap default when wrap is off', () => {
    render(<Fixture />)
    const table = screen.getByRole('table')
    expect(table).not.toHaveAttribute('data-wrap')
    // The nowrap rule itself lives in CSS; the attribute is the contract.
    expect(screen.getByRole('columnheader')).not.toHaveAttribute('data-wrap')
  })

  it('marks the table as wrapping and keeps it in the scroll port', () => {
    const { container } = render(<Fixture wrap />)
    const table = screen.getByRole('table')
    expect(table).toHaveAttribute('data-wrap', 'true')

    const port = container.querySelector('[data-slot="table-container"]')
    expect(port).not.toBeNull()
    expect(port).toHaveAttribute('data-scroll-port', 'true')
    // The hint is what the port's trailing edge fade is painted from.
    expect(
      port?.querySelector('[data-slot="table-scroll-hint"]')
    ).not.toBeNull()
    expect(
      port?.querySelector('[data-slot="table-scroll-hint-bar"]')
    ).not.toBeNull()
  })

  it('renders the table bare when the caller supplies its own scroll port', () => {
    const { container } = render(<Fixture wrap withContainer={false} />)
    expect(container.querySelector('[data-slot="table-container"]')).toBeNull()
    expect(screen.getByRole('table')).toHaveAttribute('data-wrap', 'true')
  })

  it('keeps the header sticky contract intact', () => {
    render(<Fixture wrap />)
    // Both the section and the cells carry the sticky band: engines differ on
    // which one it applies to once `border-collapse: collapse` is in play.
    const headerSection = screen.getAllByRole('rowgroup')[0]
    expect(headerSection).toHaveAttribute('data-slot', 'table-header')
    expect(screen.getByRole('columnheader')).toHaveAttribute(
      'data-slot',
      'table-head'
    )
  })
})
