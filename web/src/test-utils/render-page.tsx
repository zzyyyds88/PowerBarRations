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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render } from '@testing-library/react'
import type { ReactElement, ReactNode } from 'react'

/**
 * Render a console page inside a real memory router.
 *
 * Workspace pages contain route links and `useLocation`, so rendering them
 * without a RouterProvider is not a valid fixture after the 5+1 IA refactor.
 * QueryClient is provided too; status is seeded so module gates resolve
 * synchronously in tests.
 */
export function renderPage(
  ui: ReactNode,
  options: {
    path?: string
    status?: Record<string, unknown>
    client?: QueryClient
  } = {}
) {
  const client =
    options.client ??
    new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
  if (!options.client) {
    client.setQueryData(['status'], {
      SidebarModulesAdmin: '',
      ...options.status,
    })
  }

  const rootRoute = createRootRoute({
    component: () => ui as ReactElement,
  })
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({
      initialEntries: [options.path ?? '/'],
    }),
  })

  return {
    client,
    router,
    ...render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    ),
  }
}
