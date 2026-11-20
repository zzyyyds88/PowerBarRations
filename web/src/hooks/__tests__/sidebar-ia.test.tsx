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

function childUrls(item: { items?: { url?: string }[] } | undefined) {
  return (item?.items ?? []).map((sub) => sub.url)
}

it('groups usage, channels and settings without flattening the menu', () => {
  const { result } = renderHook(() => useSidebarData())
  const general = result.current.navGroups.find(
    (group) => group.id === 'general'
  )
  const admin = result.current.navGroups.find((group) => group.id === 'admin')

  expect(
    childUrls(general?.items.find((item) => item.title === 'Nav usage'))
  ).toEqual(['/logs/requests', '/logs/sessions', '/logs/context'])
  expect(
    childUrls(
      general?.items.find((item) => item.title === 'Nav channels and models')
    )
  ).toEqual([
    '/connections/channels',
    '/connections/health',
    '/connections/catalog',
  ])
  // 概览是操作员看完实时拓扑后的第一个落点，因此直接排在实时路由之后。
  expect(general?.items.map((item) => item.title)).toEqual([
    'Nav live routing',
    'Nav overview',
    'Nav routing',
    'Nav channels and models',
    'Nav api keys',
    'Nav usage',
  ])
  // 脱敏是实例级配置，不是日志视图；它属于系统区，不在用量分组里。
  expect(admin?.items.map((item) => item.title)).toEqual([
    'Nav masking',
    'Nav audit logs',
    'Nav system tasks',
    'Nav settings',
  ])
  expect(
    childUrls(admin?.items.find((item) => item.title === 'Nav settings'))
  ).toEqual([
    '/system-settings/site',
    '/system-settings/models',
    '/system-settings/content',
    '/system-settings/operations',
  ])
})

it('keeps playground and retired paths out of the primary navigation', () => {
  const { result } = renderHook(() => useSidebarData())
  const urls = result.current.navGroups.flatMap((group) =>
    group.items.flatMap((item) =>
      item.url ? [item.url] : (item.items ?? []).map((sub) => sub.url)
    )
  )

  expect(urls).not.toContain('/playground')
  expect(urls).not.toContain('/usage-logs/audit')
  expect(urls).not.toContain('/system-tasks')
  expect(urls).not.toContain('/channels')
  expect(urls).not.toContain('/channel-status')
  expect(urls).not.toContain('/models/metadata')
})
