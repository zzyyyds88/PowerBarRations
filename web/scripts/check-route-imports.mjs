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
import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const routeTreePath = resolve(webRoot, 'src/routeTree.gen.ts')

if (!existsSync(routeTreePath)) {
  console.error(`[route-imports] missing ${routeTreePath}`)
  process.exit(1)
}

const source = readFileSync(routeTreePath, 'utf8')
const importPattern = /from\s+'(\.{1,2}\/[^']+)'/g
const candidates = ['.ts', '.tsx', '/index.ts', '/index.tsx']

const missing = []
let match
while ((match = importPattern.exec(source)) !== null) {
  const specifier = match[1]
  const base = resolve(dirname(routeTreePath), specifier)
  const found = [base, ...candidates.map((suffix) => base + suffix)].some(
    (candidate) => existsSync(candidate)
  )
  if (!found) {
    missing.push(specifier)
  }
}

if (missing.length > 0) {
  console.error('[route-imports] routeTree.gen.ts references missing files:')
  for (const specifier of missing) {
    console.error(`  - ${specifier}`)
  }
  process.exit(1)
}

console.log(
  `[route-imports] ok (${(source.match(importPattern) ?? []).length} relative imports)`
)
