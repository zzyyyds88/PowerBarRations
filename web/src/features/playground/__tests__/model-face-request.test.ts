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
import { afterEach, describe, expect, test, vi } from 'vitest'

import { API_ENDPOINTS } from '../constants'
import { sendChatCompletion } from '../api'
import type { ChatCompletionRequest } from '../types'

const payload: ChatCompletionRequest = {
  model: 'model-1',
  messages: [{ role: 'user', content: 'ping' }],
  stream: false,
}

describe('playground model-face request', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  test('非流式请求打 /v1/chat/completions 且只带客户端密钥', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ id: 'x', choices: [] }),
    })
    vi.stubGlobal('fetch', fetchMock)

    await sendChatCompletion(payload, 'pbr-client-key')

    expect(API_ENDPOINTS.CHAT_COMPLETIONS).toBe('/v1/chat/completions')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/v1/chat/completions')
    expect(init.method).toBe('POST')
    expect(init.credentials).toBe('omit')
    expect((init.headers as Record<string, string>).Authorization).toBe(
      'Bearer pbr-client-key'
    )
    expect(JSON.parse(String(init.body))).toMatchObject({ model: 'model-1' })
  })

  test('失败响应带上服务端 message，便于用户定位', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 403,
      json: async () => ({
        error: { message: "client key 'a' is not allowed to use lane 'l'" },
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    await expect(sendChatCompletion(payload, 'pbr-client-key')).rejects.toThrow(
      "403: client key 'a' is not allowed to use lane 'l'"
    )
  })

  test('非 JSON 错误响应回落成状态码', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => {
        throw new Error('not json')
      },
    })
    vi.stubGlobal('fetch', fetchMock)

    await expect(sendChatCompletion(payload, 'pbr-client-key')).rejects.toThrow(
      'HTTP 502'
    )
  })
})
