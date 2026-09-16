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
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { CommonLogsFilterBar } from '../common-logs-filter-bar'
import { UsageLogsProvider } from '../usage-logs-provider'

// PBR 单用户网关没有"用户分组"概念（design-v1 §1.3），控制台的分组筛选只保留
// 一个默认分组；这里保护"可用值、URL 往返、历史值可编辑"这几条稳定行为。

const pointerCaptureDescriptor = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  'setPointerCapture'
)

function FilterFixture() {
  // eslint-disable-next-line react/incompatible-library -- test fixture only
  const table = useReactTable({
    data: [],
    columns: [],
    getCoreRowModel: getCoreRowModel(),
  })
  return (
    <UsageLogsProvider>
      <CommonLogsFilterBar table={table} />
    </UsageLogsProvider>
  )
}

async function renderFilter(initialEntry = '/usage-logs/common') {
  // 分组不再走网络；其余请求返回空统计，避免未预期的真实调用。
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: { success: true, data: { quota: 0, rpm: 0, tpm: 0 } },
  }))
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const logs = createRoute({
    getParentRoute: () => auth,
    path: '/usage-logs/$section',
    component: FilterFixture,
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([logs])]),
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  if (window.matchMedia('(max-width: 640px)').matches) {
    await userEvent.click(
      await screen.findByRole('button', { name: /^Filter/ })
    )
  }
  await screen.findByRole('combobox', { name: 'Group' })
  return router
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.setUser(null)
  if (pointerCaptureDescriptor) {
    Object.defineProperty(
      HTMLElement.prototype,
      'setPointerCapture',
      pointerCaptureDescriptor
    )
  } else {
    Reflect.deleteProperty(HTMLElement.prototype, 'setPointerCapture')
  }
})

it('offers only the default group without calling a groups endpoint', async () => {
  await renderFilter()
  const input = screen.getByRole('combobox', { name: 'Group' })
  await userEvent.click(input)
  expect(await screen.findByRole('option', { name: 'default' })).toBeVisible()
  expect(api.get).not.toHaveBeenCalledWith('/api/user/self/groups')
  expect(api.get).not.toHaveBeenCalledWith('/api/group/')
})

it('submits the default group only after Search', async () => {
  const router = await renderFilter()
  const input = screen.getByRole('combobox', { name: 'Group' })
  await userEvent.click(input)
  await userEvent.click(await screen.findByRole('option', { name: 'default' }))
  expect(input).toHaveValue('default')
  expect(router.state.location.search).not.toHaveProperty('group')
  await userEvent.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(router.state.location.search).toMatchObject({
      group: 'default',
      page: 1,
    })
  )
})

it('restores a group from the URL and clears it on Reset', async () => {
  const router = await renderFilter('/usage-logs/common?group=default')
  const input = screen.getByRole('combobox', { name: 'Group' })
  expect(input).toHaveValue('default')
  await userEvent.click(screen.getByRole('button', { name: 'Reset' }))
  await waitFor(() => expect(input).toHaveValue(''))
  expect(router.state.location.search).not.toHaveProperty('group')
})

it('keeps a historical value editable and searchable when it is not a choice', async () => {
  const router = await renderFilter('/usage-logs/common?group=retired')
  const input = screen.getByRole('combobox', { name: 'Group' })
  expect(input).toHaveValue('retired')
  await userEvent.clear(input)
  await userEvent.type(input, 'historical')
  await userEvent.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(router.state.location.search).toMatchObject({ group: 'historical' })
  )
})

it('keeps the compact input and masks the dropdown with the sensitive filters', async () => {
  await renderFilter()
  const input = screen.getByRole('combobox', { name: 'Group' })
  expect(input).toHaveClass('h-8', 'text-sm', 'leading-5')
  await userEvent.click(screen.getByRole('button', { name: /^Hide$/ }))
  await userEvent.click(input)
  const option = await screen.findByRole('option', { name: 'default' })
  const maskedField = input.closest('.\\[-webkit-text-security\\:disc\\]')
  expect(maskedField).not.toBeNull()
  expect(maskedField).toContainElement(option)
  await userEvent.keyboard('{Escape}')
  expect(input).toHaveAttribute('aria-expanded', 'false')
})

it('lets mobile users pick the default group in the filter drawer', async () => {
  Object.defineProperty(HTMLElement.prototype, 'setPointerCapture', {
    configurable: true,
    value: vi.fn(),
  })
  const originalMatchMedia = window.matchMedia
  vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
    ...originalMatchMedia(query),
    matches: query === '(max-width: 640px)',
  }))
  const router = await renderFilter()
  const dialog = screen.getByRole('dialog')
  const input = within(dialog).getByRole('combobox', { name: 'Group' })
  await userEvent.click(input)
  await userEvent.click(await within(dialog).findByRole('option', { name: 'default' }))
  expect(input).toHaveValue('default')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(router.state.location.search).toMatchObject({ group: 'default' })
  )
})
