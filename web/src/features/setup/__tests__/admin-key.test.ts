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
import { createHash } from 'node:crypto'

import { afterEach, expect, test, vi } from 'vitest'

import { deriveAdminKey } from '../api'

// 初始化页在无 Web Crypto 的非安全上下文（HTTP 局域网访问）也必须能
// 算出管理密钥，且与 subtle 路径结果一致（token-spec §2.1）。

function oracle(password: string): string {
  return createHash('sha256')
    .update(Buffer.from(password, 'utf8'))
    .digest('base64')
}

afterEach(() => {
  vi.unstubAllGlobals()
})

test('derives Base64(SHA256(password)) without Web Crypto (insecure context)', async () => {
  vi.stubGlobal('crypto', {})
  await expect(deriveAdminKey('Smoke-Password-2026!')).resolves.toBe(
    oracle('Smoke-Password-2026!')
  )
})

test('prefers crypto.subtle and agrees with the fallback', async () => {
  const digest = vi.fn(
    async (_alg: string, data: Uint8Array) =>
      new Uint8Array(createHash('sha256').update(Buffer.from(data)).digest())
        .buffer
  )
  vi.stubGlobal('crypto', { subtle: { digest } })
  await expect(deriveAdminKey('abc')).resolves.toBe(oracle('abc'))
  expect(digest).toHaveBeenCalled()
})
