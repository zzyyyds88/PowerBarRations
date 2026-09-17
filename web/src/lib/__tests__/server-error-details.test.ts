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
import { AxiosError, AxiosHeaders } from 'axios'
import { toast } from 'sonner'
import { afterEach, expect, it, vi } from 'vitest'

import { handleServerError } from '@/lib/handle-server-error'
import { AuthOperationError } from '@/lib/secure-verification'
import { getServerErrorDetails } from '@/lib/server-error-message'

afterEach(() => {
  vi.restoreAllMocks()
})

it('extracts the stable code, human message and hint from the error envelope', () => {
  const details = getServerErrorDetails({
    response: {
      data: {
        error: {
          code: 'channel_not_found',
          message: "member channel 'alpha' not found",
          hint: 'PUT /api/v1/channels/alpha',
        },
      },
    },
  })

  expect(details).toEqual({
    message: "member channel 'alpha' not found",
    code: 'channel_not_found',
    hint: 'PUT /api/v1/channels/alpha',
  })
})

it('never exposes an axios transport code as the server error code', () => {
  const details = getServerErrorDetails(
    new AxiosError('Network Error', 'ERR_NETWORK')
  )

  expect(details.code).toBeUndefined()
  expect(details.hint).toBeUndefined()
})

it('keeps an AuthOperationError reduced to its safe message without code or hint', () => {
  const original = new AxiosError(
    'HTTP 500',
    'ERR_BAD_RESPONSE',
    undefined,
    undefined,
    {
      data: {
        error: {
          code: 'internal_error',
          message: 'private server detail',
          hint: 'run migration 42',
        },
      },
      status: 500,
      statusText: 'Error',
      headers: {},
      config: { headers: new AxiosHeaders() },
    }
  )
  const wrapper = new Error('Operation failed', {
    cause: AuthOperationError.from(original),
  })

  const details = getServerErrorDetails(wrapper)

  expect(details.message).toBe('Please try again later.')
  expect(details.code).toBeUndefined()
  expect(details.hint).toBeUndefined()
})

it('shows the server hint with its code as the toast description', () => {
  const notify = vi.spyOn(toast, 'error').mockReturnValue('error')

  handleServerError({
    success: false,
    error: {
      code: 'lane_has_no_members',
      message: 'lane has no members',
      hint: 'add at least one member channel',
    },
  })

  expect(notify).toHaveBeenCalledWith('lane has no members', {
    description: 'add at least one member channel (lane_has_no_members)',
  })
})
