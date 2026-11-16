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
import { expect, it } from 'vitest'

import { BarChart } from '@/components/charts'

const data = [
  { key: 'a', label: 'Oct 1', value: 4 },
  { key: 'b', label: 'Oct 2', value: 8 },
]

it('renders one accessible plot with the shared axis and values', () => {
  const { container } = render(
    <BarChart
      data={data}
      valueFormatter={(value) => `${value} calls`}
      ariaLabel='Calls per day'
    />
  )

  expect(screen.getByRole('img', { name: 'Calls per day' })).toBeInTheDocument()
  // The screen-reader list mirrors the hover tooltip values.
  const list = container.querySelector('ul')
  expect(list?.textContent).toContain('Oct 1 · 4 calls')
  expect(list?.textContent).toContain('Oct 2 · 8 calls')
})
