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

// ui-spec §6.3/§5：侧边栏「模型管理」排在「系统任务」之后（用户指定的菜单顺序）；
// 模型入口仍只有一个，标题与模型页标题同键（'Model management'）。
it('exposes a single models entry placed right after system tasks', () => {
  const { result } = renderHook(() => useSidebarData())
  const adminItems = result.current.navGroups.find(
    (group) => group.id === 'admin'
  )?.items
  expect(adminItems).toBeDefined()

  const urls = adminItems?.map((item) => item.url) ?? []
  expect(urls.filter((url) => url?.startsWith('/models'))).toEqual([
    '/models/metadata',
  ])

  const tasksIndex = urls.indexOf('/system-tasks')
  const modelsIndex = urls.indexOf('/models/metadata')
  expect(tasksIndex).toBeGreaterThan(-1)
  expect(modelsIndex).toBe(tasksIndex + 1)

  // 侧边栏「模型管理」标题与模型页标题共用同一 i18n 键，两处显示严格一致。
  const modelsEntry = adminItems?.find((item) =>
    item.url?.startsWith('/models')
  )
  expect(modelsEntry?.title).toBe('Model management')
})

// ui-spec §5/§6.10：「系统信息」页删除，任务面板提为「系统任务」独立页。
// PBR 单用户，管理面即全量权限，入口不带任何角色门。
it('replaces the system info entry with an ungated system tasks page', () => {
  const { result } = renderHook(() => useSidebarData())
  const adminItems = result.current.navGroups.find(
    (group) => group.id === 'admin'
  )?.items
  expect(adminItems).toBeDefined()

  const urls = adminItems?.map((item) => item.url) ?? []
  expect(urls).not.toContain('/system-info')
  expect(urls).toContain('/system-tasks')

  const tasksEntry = adminItems?.find((item) => item.url === '/system-tasks')
  expect(tasksEntry?.title).toBe('System Tasks')
  expect(tasksEntry).not.toHaveProperty('requiredRole')
})
