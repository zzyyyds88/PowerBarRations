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

import { STORAGE_KEYS } from '../../../constants'
import { loadConfig, saveConfig } from '../storage'

function readPersistedConfig(): Record<string, unknown> {
  const raw = localStorage.getItem(STORAGE_KEYS.CONFIG)
  if (!raw) return {}
  return (JSON.parse(raw) as { data?: Record<string, unknown> }).data ?? {}
}

describe('playground client key storage', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  it('keeps clientKey out of localStorage and stores it in sessionStorage', () => {
    saveConfig({ model: 'gpt-4o', clientKey: 'sk-secret', temperature: 0.5 })

    expect(readPersistedConfig()).toMatchObject({
      model: 'gpt-4o',
      temperature: 0.5,
    })
    expect(readPersistedConfig()).not.toHaveProperty('clientKey')
    expect(sessionStorage.getItem(STORAGE_KEYS.CLIENT_KEY)).toBe('sk-secret')
  })

  it('restores the clientKey from sessionStorage on load', () => {
    saveConfig({ model: 'gpt-4o', clientKey: 'sk-secret', temperature: 0.5 })

    expect(loadConfig()).toMatchObject({
      model: 'gpt-4o',
      clientKey: 'sk-secret',
      temperature: 0.5,
    })
  })

  it('drops a legacy clientKey that was persisted in localStorage', () => {
    localStorage.setItem(
      STORAGE_KEYS.CONFIG,
      JSON.stringify({
        version: 1,
        data: { model: 'legacy-model', clientKey: 'legacy-secret' },
      })
    )

    expect(loadConfig()).toMatchObject({ model: 'legacy-model' })
    expect(loadConfig().clientKey).toBe('')
    expect(readPersistedConfig()).not.toHaveProperty('clientKey')
  })

  it('clears the session clientKey when saved empty', () => {
    saveConfig({ clientKey: 'sk-secret' })
    expect(sessionStorage.getItem(STORAGE_KEYS.CLIENT_KEY)).toBe('sk-secret')

    saveConfig({ clientKey: '' })
    expect(sessionStorage.getItem(STORAGE_KEYS.CLIENT_KEY)).toBeNull()
  })
})
