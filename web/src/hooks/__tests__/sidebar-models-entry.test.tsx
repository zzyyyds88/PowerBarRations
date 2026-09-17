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

// ui-spec §6.3：「路由与故障切换」是侧边栏独立页（/routes），紧跟在「模型」之后；
// 模型入口仍只有一个。
it('exposes one models entry followed by the routing & failover page', () => {
  const { result } = renderHook(() => useSidebarData())
  const adminItems = result.current.navGroups.find(
    (group) => group.id === 'admin'
  )?.items
  expect(adminItems).toBeDefined()

  const urls = adminItems?.map((item) => item.url) ?? []
  expect(urls.filter((url) => url?.startsWith('/models'))).toEqual([
    '/models/metadata',
  ])

  const routingIndex = urls.indexOf('/routes')
  expect(routingIndex).toBeGreaterThan(-1)
  expect(urls[routingIndex - 1]).toBe('/models/metadata')
  expect(adminItems?.[routingIndex]?.title).toBe('Routing & Failover')
})
