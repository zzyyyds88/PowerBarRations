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

// ui-spec §6.4A：渠道状态是独立的一等入口，排在 admin 组「渠道管理」之后，
// 标题与页面标题同键（'Channel status'）；它不是渠道行抽屉里的隐藏入口。
it('exposes the channel status page right after channel management', () => {
  const { result } = renderHook(() => useSidebarData())
  const adminItems = result.current.navGroups.find(
    (group) => group.id === 'admin'
  )?.items
  expect(adminItems).toBeDefined()

  const urls = adminItems?.map((item) => item.url) ?? []
  const channelsIndex = urls.indexOf('/channels')
  expect(channelsIndex).toBeGreaterThan(-1)
  expect(urls[channelsIndex + 1]).toBe('/channel-status')

  const statusEntry = adminItems?.find((item) => item.url === '/channel-status')
  expect(statusEntry?.title).toBe('Channel status')
  // 渠道模块被管理员隐藏时，渠道状态页一并隐藏（同一 module）。
  expect(statusEntry).not.toHaveProperty('requiredRole')
})
