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
  createRootRoute,
  createRoute,
  createMemoryHistory,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { UsageLogsProvider } from '../usage-logs-provider'
import { UsageLogsTable } from '../usage-logs-table'

// 选路前失败（缺少 X-PBR-Lane）的日志没有渠道：界面必须显示 `—`，
// 不能把占位 0 渲染成「渠道 #0」误导成渠道故障。
it('shows a dash instead of #0 for logs rejected before routing', async () => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (String(url).includes('/logs')) {
      return {
        data: {
          items: [
            {
              id: 9,
              ts: '2026-11-07T12:19:00Z',
              type: 5,
              success: false,
              lane: '',
              request_model: '',
              channel_id: 0,
              channel: '',
              key_name: 'DSH',
              http_status: 400,
              error_kind: 'client_error',
              error_summary: 'missing X-PBR-Lane header',
              total_attempts: 0,
              attempts: [],
              prompt_tokens: 0,
              completion_tokens: 0,
              total_ms: 0,
              ttft_ms: 0,
              estimated_cost: 0,
              is_stream: false,
              inbound_format: 'openai',
              route_source: '',
              price_band: -1,
              cache_read_tokens: 0,
              cache_write_tokens: 0,
              reasoning_tokens: 0,
            },
          ],
          total: 1,
          page: 1,
          page_size: 100,
        },
      }
    }
    return { data: { items: [] } }
  })

  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const root = createRootRoute()
  const authenticated = createRoute({
    getParentRoute: () => root,
    id: '_authenticated',
  })
  const logs = createRoute({
    getParentRoute: () => authenticated,
    path: 'logs/requests',
    component: () => (
      <UsageLogsProvider>
        <UsageLogsTable logCategory='pbr' />
      </UsageLogsProvider>
    ),
  })
  const router = createRouter({
    routeTree: root.addChildren([authenticated.addChildren([logs])]),
    history: createMemoryHistory({ initialEntries: ['/logs/requests'] }),
  })
  await router.load()

  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )

  // 等待表格渲染出这一行（DSH 密钥名），再断言渠道占位不再显示 `#0`。
  expect(await screen.findByText('DSH')).toBeInTheDocument()
  expect(screen.queryByText('#0')).not.toBeInTheDocument()
})
