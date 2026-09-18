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
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { SystemTask } from '@/features/system-settings/types'

import { SystemTasks } from '../index'

vi.mock('@/features/system-settings/api', () => ({
  listSystemTasks: vi.fn(),
}))

async function listSystemTasksMock() {
  const { listSystemTasks } = await import('@/features/system-settings/api')
  return vi.mocked(listSystemTasks)
}

function makeTask(overrides: Partial<SystemTask> = {}): SystemTask {
  return {
    id: 1,
    task_id: 'task-1',
    type: 'log_cleanup',
    status: 'running',
    state: { progress: 40 },
    created_at: 1,
    updated_at: 1,
    ...overrides,
  }
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <SystemTasks />
    </QueryClientProvider>
  )
  return client
}

afterEach(() => {
  cleanup()
})

// ui-spec §6.10：系统任务页标题为「系统任务」，不出现 "Root" 徽标与角色门。
test('renders the system tasks page title without a Root badge', async () => {
  const mock = await listSystemTasksMock()
  mock.mockResolvedValue([])

  renderPage()

  expect(
    await screen.findByRole('heading', { level: 2, name: 'System Tasks' })
  ).toBeVisible()
  expect(screen.queryByText('Root')).not.toBeInTheDocument()
  expect(screen.queryByText('System Info')).not.toBeInTheDocument()
})

// ui-spec §6.10：任务面板分活跃/历史两节；活跃任务进度可见。
test('splits active and historical tasks into separate sections', async () => {
  const mock = await listSystemTasksMock()
  mock.mockResolvedValue([
    makeTask({ task_id: 'active-1', status: 'running' }),
    makeTask({ task_id: 'done-1', status: 'succeeded', state: {} }),
  ])

  renderPage()

  expect(await screen.findByText('Active Tasks')).toBeVisible()
  expect(screen.getByText('Task History')).toBeVisible()
  expect(screen.getByText('40%')).toBeVisible()
})

// ui-spec §6.10：采用上游 task-logs 的详细形态，详情展开 payload/state/result/error。
test('opens the task details dialog with payload, state, result and error', async () => {
  const mock = await listSystemTasksMock()
  mock.mockResolvedValue([
    makeTask({
      task_id: 'cleanup-9',
      status: 'failed',
      payload: { target_timestamp: 1700000000, batch_size: 500 },
      state: { processed: 250, progress: 50 },
      result: { deleted_count: 250 },
      error: 'database is locked',
    }),
  ])

  renderPage()
  const user = userEvent.setup()
  await user.click(await screen.findByRole('button', { name: 'View details' }))

  const dialog = await screen.findByRole('dialog', { name: 'Task Details' })
  expect(within(dialog).getByText('Payload')).toBeVisible()
  expect(within(dialog).getByText('State')).toBeVisible()
  expect(within(dialog).getByText('Result')).toBeVisible()
  expect(within(dialog).getByText('database is locked')).toBeVisible()
  expect(within(dialog).getByText(/target_timestamp/)).toBeVisible()
})

// ui-spec §6.10：task-plugins 子系统已删，任务详情不得出现 plugin/作者/厂商。
test('task details never mention plugins, authors or vendors', async () => {
  const mock = await listSystemTasksMock()
  mock.mockResolvedValue([
    makeTask({
      task_id: 'cleanup-1',
      status: 'succeeded',
      payload: { batch_size: 100 },
    }),
  ])

  renderPage()
  const user = userEvent.setup()
  await user.click(await screen.findByRole('button', { name: 'View details' }))

  const dialog = await screen.findByRole('dialog', { name: 'Task Details' })
  for (const forbidden of ['Plugin', 'Plugin author', 'Vendor']) {
    expect(within(dialog).queryByText(forbidden)).toBeNull()
  }
})
