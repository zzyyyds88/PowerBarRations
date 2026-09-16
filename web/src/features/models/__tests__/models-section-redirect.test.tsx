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
} from '@tanstack/react-router'
import { afterEach, describe, expect, it } from 'vitest'

import { Route as ModelsSectionRoute } from '@/routes/_authenticated/models/$section'
import { useAuthStore } from '@/stores/auth-store'

function buildRouter(initialPath: string) {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
  const root = createRootRoute()
  const authenticated = createRoute({
    getParentRoute: () => root,
    id: '_authenticated',
  })
  // Mirror the generated route tree: re-parent the real route (and keep its
  // production beforeLoad) under a minimal authenticated parent.
  const modelsSection = ModelsSectionRoute.update({
    id: '/models/$section',
    path: '/models/$section',
    getParentRoute: () => authenticated,
  } as Parameters<typeof ModelsSectionRoute.update>[0])
  return createRouter({
    routeTree: root.addChildren([authenticated.addChildren([modelsSection])]),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  })
}

afterEach(() => {
  useAuthStore.getState().auth.reset()
})

describe('legacy models sections', () => {
  it.each([
    '/models/routing',
    '/models/vendors',
    '/models/deployments',
    '/models/deployment',
  ])('redirects %s to the flat metadata list', async (path) => {
    const router = buildRouter(path)
    await router.load()
    expect(router.state.location.pathname).toBe('/models/metadata')
  })

  it('keeps the metadata section reachable', async () => {
    const router = buildRouter('/models/metadata')
    await router.load()
    expect(router.state.location.pathname).toBe('/models/metadata')
  })
})
