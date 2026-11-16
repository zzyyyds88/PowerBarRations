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
import { describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { getUserModels } from '../api'
import { isChatCapableModel } from '../lib/options/playground-option-utils'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

describe('playground model filtering', () => {
  test('excludes embedding / rerank / audio / image models', () => {
    for (const name of [
      'bge-m3',
      'bge-m3:latest',
      'text-embedding-3-large',
      'nomic-embed-text',
      'rerank-v1',
      'whisper-1',
      'tts-1',
      'dall-e-3',
      'flux-dev',
    ]) {
      expect(isChatCapableModel(name), name).toBe(false)
    }
  })

  test('keeps normal chat model and lane names', () => {
    for (const name of [
      'deepseek-v4.1-flash',
      'glm-5.3-flash',
      'hermes-fund',
      'gpt-4o',
      'claude-3-5-sonnet',
    ]) {
      expect(isChatCapableModel(name), name).toBe(true)
    }
  })

  test('getUserModels only returns routable chat-capable lanes', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: {
        items: [
          { model: 'bge-m3', routable: true },
          { model: 'deepseek-v4.1-flash', routable: true },
          { model: 'not-routable', routable: false },
          { model: 'whisper-1', routable: true },
        ],
      },
    } as never)

    await expect(getUserModels()).resolves.toEqual([
      { label: 'deepseek-v4.1-flash', value: 'deepseek-v4.1-flash' },
    ])
  })
})
