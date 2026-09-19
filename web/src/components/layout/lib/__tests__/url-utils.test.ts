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
import { describe, expect, it } from 'vitest'

import type { NavLink } from '../../types'
import { checkIsActive } from '../url-utils'

// ui-spec §6.2：数据看板是单一入口 + 页内 Tab（overview/models/cost）。
// activeUrls 必须前缀匹配，切到任一 Tab 都保持侧边栏高亮。
describe('checkIsActive activeUrls prefix matching', () => {
  const dashboardEntry: NavLink = {
    title: 'Dashboard',
    url: '/dashboard/overview',
    activeUrls: ['/dashboard'],
  }

  it('highlights the dashboard entry on every dashboard tab', () => {
    expect(checkIsActive('/dashboard/overview', dashboardEntry)).toBe(true)
    expect(checkIsActive('/dashboard/models', dashboardEntry)).toBe(true)
    expect(checkIsActive('/dashboard/cost', dashboardEntry)).toBe(true)
  })

  it('does not highlight unrelated routes or prefix-lookalike paths', () => {
    expect(checkIsActive('/keys', dashboardEntry)).toBe(false)
    // '/dashboardx' 不是 '/dashboard' 的子路径，不得误命中。
    expect(checkIsActive('/dashboardx', dashboardEntry)).toBe(false)
  })

  it('keeps exact activeUrls entries matching only themselves', () => {
    const entry: NavLink = {
      title: 'Keys',
      url: '/keys',
      activeUrls: ['/keys/audit'],
    }
    expect(checkIsActive('/keys/audit', entry)).toBe(true)
    expect(checkIsActive('/keys/audit/123', entry)).toBe(true)
    expect(checkIsActive('/keys/other', entry)).toBe(false)
  })
})
