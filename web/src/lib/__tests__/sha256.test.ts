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

import { expect, test } from 'vitest'

import { sha256Hex } from '../sha256'

// 纯 JS SHA-256 兜底必须与标准实现逐字节一致（管理密钥派生依赖它）。
const CASES: string[] = [
  '',
  'abc',
  'a'.repeat(55),
  'a'.repeat(56),
  'a'.repeat(63),
  'a'.repeat(64),
  'a'.repeat(65),
  'a'.repeat(1000),
  '口令-with-mixed-unicode-😀',
]

test.each(CASES)('matches the node oracle for %s', (input) => {
  const bytes = new TextEncoder().encode(input)
  const oracle = createHash('sha256').update(Buffer.from(bytes)).digest('hex')
  expect(sha256Hex(bytes)).toBe(oracle)
})

test('matches published SHA-256 vectors', () => {
  expect(sha256Hex(new TextEncoder().encode('abc'))).toBe(
    'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'
  )
  expect(sha256Hex(new TextEncoder().encode(''))).toBe(
    'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
  )
})
