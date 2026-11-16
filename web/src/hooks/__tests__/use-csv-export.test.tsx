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
import { act, renderHook } from '@testing-library/react'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useCsvExport } from '@/hooks/use-csv-export'
import { api } from '@/lib/api'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn() },
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

const mockedGet = vi.mocked(api.get)

describe('useCsvExport', () => {
  beforeEach(() => {
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:pbr')
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(
      () => undefined
    )
  })

  afterEach(() => {
    vi.clearAllMocks()
    vi.restoreAllMocks()
  })

  it('downloads the CSV and keeps the server filename', async () => {
    mockedGet.mockResolvedValue({
      data: new Blob(['name\nchannel-a\n']),
      headers: { 'content-disposition': 'attachment; filename="channels.csv"' },
    } as never)
    const { result } = renderHook(() => useCsvExport())

    await act(async () => {
      await result.current.exportCsv({
        path: '/api/channels/export',
        params: { format: 'csv' },
        fallbackFilename: 'fallback.csv',
      })
    })

    expect(mockedGet).toHaveBeenCalledWith('/api/channels/export', {
      params: { format: 'csv' },
      responseType: 'blob',
      timeout: 0,
    })
    expect(URL.createObjectURL).toHaveBeenCalledTimes(1)
    expect(HTMLAnchorElement.prototype.click).toHaveBeenCalledTimes(1)
    expect(result.current.isExporting).toBe(false)
    expect(toast.error).not.toHaveBeenCalled()
  })

  it('surfaces export_too_large as a toast instead of a silent failure', async () => {
    mockedGet.mockRejectedValue({
      isAxiosError: true,
      response: {
        status: 400,
        data: new Blob([
          JSON.stringify({
            error: { code: 'export_too_large', message: 'too many rows' },
          }),
        ]),
      },
    })
    const { result } = renderHook(() => useCsvExport())

    await act(async () => {
      await result.current.exportCsv({
        path: '/api/audit/export',
        params: { format: 'csv', from: 'a', to: 'b' },
        fallbackFilename: 'audit.csv',
      })
    })

    expect(toast.error).toHaveBeenCalledWith(
      'Too many rows to export. Narrow the time range or filters and try again.'
    )
    expect(URL.createObjectURL).not.toHaveBeenCalled()
  })
})
