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
import { beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { batchDeleteApiKeys } from '../api'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    put: vi.fn(),
    post: vi.fn(),
    delete: vi.fn(),
  },
}))

beforeEach(() => {
  vi.mocked(api.get).mockResolvedValue({
    data: {
      items: [
        { id: 1, name: 'alpha' },
        { id: 2, name: 'beta' },
      ],
    },
  })
})

it('aggregates successes and failures instead of reporting all success', async () => {
  vi.mocked(api.delete).mockImplementation(async (url: string) => {
    if (url === '/api/keys/beta') throw new Error('locked')
    return { data: {} }
  })

  const result = await batchDeleteApiKeys([1, 2])

  expect(result.success).toBe(true)
  expect(result.data).toEqual({
    deleted: 1,
    failed: 1,
    failedNames: ['beta'],
  })
})

it('reports zero failures when every delete succeeds', async () => {
  vi.mocked(api.delete).mockResolvedValue({ data: {} })

  const result = await batchDeleteApiKeys([1, 2])

  expect(result.data).toEqual({ deleted: 2, failed: 0, failedNames: [] })
})
