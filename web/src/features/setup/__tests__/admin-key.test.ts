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
import { webcrypto } from 'node:crypto'

import { beforeAll, expect, it } from 'vitest'

import { deriveAdminKey } from '../api'

const EXPECTED = 'XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg='

beforeAll(() => {
  Object.defineProperty(globalThis, 'crypto', {
    configurable: true,
    value: webcrypto,
  })
})

it('derives Base64(SHA256(password)) with standard padding', async () => {
  expect(await deriveAdminKey('password')).toBe(EXPECTED)
})

it('returns null when Web Crypto is unavailable', async () => {
  const original = globalThis.crypto
  Object.defineProperty(globalThis, 'crypto', {
    configurable: true,
    value: {},
  })
  expect(await deriveAdminKey('password')).toBeNull()
  Object.defineProperty(globalThis, 'crypto', {
    configurable: true,
    value: original,
  })
})
