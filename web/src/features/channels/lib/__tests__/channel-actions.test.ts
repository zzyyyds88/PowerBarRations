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

describe('channel delete lane-reference guards', () => {
  it('keeps the channel on conflict and returns the referencing lane names', async () => {
    vi.mocked(deleteChannel).mockResolvedValue({
      success: false,
      code: 'conflict',
      message: '渠道被以下车道引用：m-3',
      data: { lanes: ['m-3'] },
    })
    const client = new QueryClient()
    const invalidate = vi.spyOn(client, 'invalidateQueries')

    const failure = await handleDeleteChannel(7, client)

    expect(failure).toMatchObject({ code: 'conflict', lanes: ['m-3'] })
    expect(invalidate).not.toHaveBeenCalled()
  })

  it('invalidates the routable-models cache after a successful delete', async () => {
    vi.mocked(deleteChannel).mockResolvedValue({ success: true })
    const client = new QueryClient()
    const invalidate = vi.spyOn(client, 'invalidateQueries')

    const failure = await handleDeleteChannel(7, client)

    expect(failure).toBeNull()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: pbrModelsQueryKey })
  })

  it('returns the blocked channel -> lanes map for a rejected batch delete', async () => {
    vi.mocked(batchDeleteChannels).mockResolvedValue({
      success: false,
      code: 'conflict',
      message: '部分渠道被车道引用',
      data: { blocked: { alpha: ['m-1'], beta: ['m-2'] } },
    })

    const failure = await handleBatchDelete([1, 2])

    expect(failure?.blocked).toEqual({ alpha: ['m-1'], beta: ['m-2'] })
  })

  it('returns the blocked map for a rejected disabled-channels purge', async () => {
    vi.mocked(deleteDisabledChannels).mockResolvedValue({
      success: false,
      code: 'conflict',
      message: '部分已禁用渠道被车道引用',
      data: { blocked: { legacy: ['m-9'] } },
    })

    const failure = await handleDeleteAllDisabled()

    expect(failure?.blocked).toEqual({ legacy: ['m-9'] })
  })
})
