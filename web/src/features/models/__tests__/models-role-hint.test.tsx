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
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'

import { ModelsRoleHint } from '../components/models-role-hint'

afterEach(cleanup)

// ui-spec §6.3：模型管理只是元数据目录，可调用性由车道决定。用户最容易在
// 这一页误以为"添加模型 = 模型可用"，所以常驻指路提示并链到两个真正落点。
it('points users from metadata-only page to channels and routing', async () => {
  const root = createRootRoute()
  const authenticated = createRoute({
    getParentRoute: () => root,
    id: '_authenticated',
  })
  const hint = createRoute({
    getParentRoute: () => authenticated,
    path: 'models/metadata',
    component: ModelsRoleHint,
  })
  const channels = createRoute({
    getParentRoute: () => authenticated,
    path: 'channels',
    component: () => null,
  })
  const routing = createRoute({
    getParentRoute: () => authenticated,
    path: 'routes',
    component: () => null,
  })
  const router = createRouter({
    routeTree: root.addChildren([
      authenticated.addChildren([hint, channels, routing]),
    ]),
    history: createMemoryHistory({ initialEntries: ['/models/metadata'] }),
  })
  await router.load()

  render(<RouterProvider router={router} />)

  expect(
    screen.getByText(/This page only maintains model metadata/)
  ).toBeVisible()
  expect(
    screen.getByRole('link', { name: 'Channel management' })
  ).toHaveAttribute('href', '/channels')
  expect(
    screen.getByRole('link', { name: 'Routing & Failover' })
  ).toHaveAttribute('href', '/routes')
})
