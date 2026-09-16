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
import { renderHook } from '@testing-library/react'
import { expect, it } from 'vitest'

import { useSidebarData } from '../use-sidebar-data'

it('exposes exactly one models entry and no independent routing entry', () => {
  const { result } = renderHook(() => useSidebarData())
  const items = result.current.navGroups.flatMap((group) => group.items)

  const modelsEntries = items.filter((item) =>
    item.url?.startsWith('/models')
  )
  expect(modelsEntries.map((item) => item.url)).toEqual(['/models/metadata'])
  expect(
    items.some((item) => item.title === 'Routing & Failover')
  ).toBe(false)
})
