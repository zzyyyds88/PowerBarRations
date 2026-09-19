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
import { beforeEach, describe, expect, it } from 'vitest'

import type { Message } from '../../../types'
import { loadMessages, saveMessages } from '../storage'

function assistantMessage(servedBy?: string): Message {
  return {
    key: 'msg-1',
    from: 'assistant',
    versions: [{ id: 'v1', content: 'hi' }],
    status: 'complete',
    servedBy,
  }
}

describe('playground message storage keeps X-Served-By（ui-spec §6.8）', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  it('round-trips servedBy so the header survives a reload', () => {
    saveMessages([assistantMessage('channel=1:channel-a, model=model-x')])

    const loaded = loadMessages()

    expect(loaded?.[0]?.servedBy).toBe('channel=1:channel-a, model=model-x')
  })

  it('omits servedBy when the header was absent', () => {
    saveMessages([assistantMessage()])

    expect(loadMessages()?.[0]?.servedBy).toBeUndefined()
  })
})
