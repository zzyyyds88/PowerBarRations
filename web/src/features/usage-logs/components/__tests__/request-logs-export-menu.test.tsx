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
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { RequestLogsExportMenu } from '../request-logs-export-menu'

vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

const search = {
  lane: 'lane-a',
  channel: 'channel-a',
  key: 'key-a',
  model: 'model-a',
  success: 'false',
  startTime: Date.UTC(2026, 9, 1, 0, 0, 0),
  endTime: Date.UTC(2026, 9, 2, 0, 0, 0),
}

let createObjectURL: ReturnType<typeof vi.fn>
let revokeObjectURL: ReturnType<typeof vi.fn>
let anchorClick: ReturnType<typeof vi.spyOn>

beforeEach(() => {
  createObjectURL = vi.fn(() => 'blob:pbr-logs')
  revokeObjectURL = vi.fn()
  Object.defineProperty(URL, 'createObjectURL', {
    configurable: true,
    value: createObjectURL,
  })
  Object.defineProperty(URL, 'revokeObjectURL', {
    configurable: true,
    value: revokeObjectURL,
  })
  anchorClick = vi
    .spyOn(HTMLAnchorElement.prototype, 'click')
    .mockImplementation(() => undefined)
})

afterEach(() => {
  anchorClick.mockRestore()
})

it('sends the applied filters to /api/logs/export and downloads the blob', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: new Blob(['ts,request_id\n']),
    headers: { 'content-disposition': 'attachment; filename="logs.csv"' },
  })
  const user = userEvent.setup()
  render(<RequestLogsExportMenu search={search} />)

  await user.click(screen.getByRole('button', { name: 'Export' }))
  await user.click(screen.getByRole('menuitem', { name: 'Export as CSV' }))

  await waitFor(() =>
    expect(get).toHaveBeenCalledWith(
      '/api/logs/export',
      expect.objectContaining({
        responseType: 'blob',
        params: {
          format: 'csv',
          from: '2026-10-01T00:00:00.000Z',
          to: '2026-10-02T00:00:00.000Z',
          lane: 'lane-a',
          channel: 'channel-a',
          key: 'key-a',
          model: 'model-a',
          success: 'false',
        },
      })
    )
  )
  expect(createObjectURL).toHaveBeenCalledTimes(1)
  expect(anchorClick).toHaveBeenCalledTimes(1)
})

it('disables the trigger and announces progress while exporting', async () => {
  let resolveExport: (value: unknown) => void = () => undefined
  let onProgress:
    | ((event: { loaded: number; total?: number }) => void)
    | undefined
  const pending = new Promise((resolve) => {
    resolveExport = resolve
  })
  vi.spyOn(api, 'get').mockImplementation(((
    _url: string,
    config?: {
      onDownloadProgress?: (event: { loaded: number; total?: number }) => void
    }
  ) => {
    onProgress = config?.onDownloadProgress
    return pending
  }) as never)
  const user = userEvent.setup()
  render(<RequestLogsExportMenu search={search} />)

  await user.click(screen.getByRole('button', { name: 'Export' }))
  await user.click(screen.getByRole('menuitem', { name: 'Export as NDJSON' }))

  const exporting = await screen.findByRole('button', {
    name: 'Exporting…',
  })
  expect(exporting).toBeDisabled()

  act(() => onProgress?.({ loaded: 50, total: 100 }))
  expect(
    await screen.findByRole('button', { name: 'Exporting… 50%' })
  ).toBeDisabled()

  resolveExport({
    data: new Blob(['{}\n']),
    headers: {},
  })
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Export' })).toBeEnabled()
  )
})

it('shows the contract export_too_large message instead of the row cap', async () => {
  vi.spyOn(api, 'get').mockRejectedValue({
    isAxiosError: true,
    response: {
      status: 400,
      data: new Blob([
        JSON.stringify({
          error: {
            code: 'export_too_large',
            message: 'matched 120000 rows',
          },
        }),
      ]),
    },
  })
  const user = userEvent.setup()
  render(<RequestLogsExportMenu search={search} />)

  await user.click(screen.getByRole('button', { name: 'Export' }))
  await user.click(screen.getByRole('menuitem', { name: 'Export as CSV' }))

  await waitFor(() =>
    expect(toast.error).toHaveBeenCalledWith('matched 120000 rows')
  )
})
