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
import { QueryClient } from '@tanstack/react-query'
import { AxiosError } from 'axios'
import { describe, expect, it, vi } from 'vitest'

import { pbrModelsQueryKey } from '@/features/routes/api'

import {
  batchDeleteChannels,
  deleteChannel,
  deleteDisabledChannels,
} from '../../api'
import {
  handleBatchDelete,
  handleDeleteAllDisabled,
  handleDeleteChannel,
} from '../channel-actions'
import {
  isChannelReferenceConflict,
  parseChannelReferenceConflict,
} from '../channel-reference-conflict'

vi.mock('../../api', () => ({
  batchDeleteChannels: vi.fn(),
  batchSetChannelTag: vi.fn(),
  copyChannel: vi.fn(),
  deleteChannel: vi.fn(),
  deleteDisabledChannels: vi.fn(),
  disableTagChannels: vi.fn(),
  enableTagChannels: vi.fn(),
  fixChannelAbilities: vi.fn(),
  testAllChannels: vi.fn(),
  testChannel: vi.fn(),
  updateChannelStatus: vi.fn(),
  batchUpdateChannelStatus: vi.fn(),
}))

/** 构造带 §3 错误包络的 axios 拒绝（新契约：失败即非 2xx）。 */
function envelopeError(
  status: number,
  code: string,
  message: string,
  details?: unknown,
  hint?: string
): AxiosError {
  const error = new AxiosError(message)
  error.response = {
    data: {
      error: {
        code,
        message,
        ...(hint ? { hint } : {}),
        ...(details ? { details } : {}),
      },
    },
    status,
    statusText: 'Error',
    headers: {},
    config: { headers: {} } as never,
  } as never
  return error
}

describe('channel reference conflict parsing', () => {
  it('recognizes the stable-surface 409 conflict with details.lanes', () => {
    const error = envelopeError(
      409,
      'conflict',
      'refusing to remove models still referenced by lanes: m1',
      { lanes: ['m1'] }
    )

    expect(isChannelReferenceConflict(error)).toBe(true)
    expect(parseChannelReferenceConflict(error).lanes).toEqual(['m1'])
  })

  it('recognizes the base-surface 409 models_referenced_by_lanes with details.lanes', () => {
    const error = envelopeError(
      409,
      'models_referenced_by_lanes',
      '以下模型仍被车道引用：m1',
      { lanes: ['m1'] }
    )

    expect(isChannelReferenceConflict(error)).toBe(true)
    expect(parseChannelReferenceConflict(error).lanes).toEqual(['m1'])
  })

  it('recognizes a 409 conflict with details.blocked (batch delete)', () => {
    const error = envelopeError(409, 'conflict', '部分渠道被车道引用', {
      blocked: { alpha: ['m-1'], beta: ['m-2'] },
    })

    expect(isChannelReferenceConflict(error)).toBe(true)
    expect(parseChannelReferenceConflict(error).blocked).toEqual({
      alpha: ['m-1'],
      beta: ['m-2'],
    })
  })

  it('falls back to the message list only when details are absent', () => {
    const error = envelopeError(409, 'conflict', '渠道被以下车道引用：m-1, m-2')

    expect(isChannelReferenceConflict(error)).toBe(true)
    expect(parseChannelReferenceConflict(error).lanes).toEqual(['m-1', 'm-2'])
  })

  it('does not treat an unrelated validation failure as a lane-reference conflict', () => {
    const error = envelopeError(
      400,
      'validation_failed',
      'channel cannot be empty'
    )

    expect(isChannelReferenceConflict(error)).toBe(false)
    expect(parseChannelReferenceConflict(error).lanes).toEqual([])
  })
})

describe('channel delete lane-reference guards', () => {
  it('keeps the channel on conflict and returns the referencing lane names', async () => {
    vi.mocked(deleteChannel).mockRejectedValue(
      envelopeError(409, 'conflict', '渠道被以下车道引用：m-3', {
        lanes: ['m-3'],
      })
    )
    const client = new QueryClient()
    const invalidate = vi.spyOn(client, 'invalidateQueries')

    const failure = await handleDeleteChannel(7, client)

    expect(failure).toMatchObject({ code: 'conflict' })
    expect(failure?.lanes).toEqual(['m-3'])
    expect(invalidate).not.toHaveBeenCalled()
  })

  it('invalidates the routable-models cache after a successful delete', async () => {
    vi.mocked(deleteChannel).mockResolvedValue(undefined)
    const client = new QueryClient()
    const invalidate = vi.spyOn(client, 'invalidateQueries')

    const failure = await handleDeleteChannel(7, client)

    expect(failure).toBeNull()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: pbrModelsQueryKey })
  })

  it('returns the blocked channel -> lanes map for a rejected batch delete', async () => {
    vi.mocked(batchDeleteChannels).mockRejectedValue(
      envelopeError(409, 'conflict', '部分渠道被车道引用', {
        blocked: { alpha: ['m-1'], beta: ['m-2'] },
      })
    )

    const failure = await handleBatchDelete([1, 2])

    expect(failure?.blocked).toEqual({ alpha: ['m-1'], beta: ['m-2'] })
  })

  it('returns the blocked map for a rejected disabled-channels purge', async () => {
    vi.mocked(deleteDisabledChannels).mockRejectedValue(
      envelopeError(409, 'conflict', '部分已禁用渠道被车道引用', {
        blocked: { legacy: ['m-9'] },
      })
    )

    const failure = await handleDeleteAllDisabled()

    expect(failure?.blocked).toEqual({ legacy: ['m-9'] })
  })
})
