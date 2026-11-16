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
import { execFileSync } from 'node:child_process'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

let output = ''
try {
  output = execFileSync(
    'git',
    ['ls-files', '--others', '--ignored', '--exclude-standard', 'src'],
    { cwd: webRoot, encoding: 'utf8' }
  )
} catch (error) {
  console.error('[gitignore] unable to inspect ignored files')
  console.error(error instanceof Error ? error.message : String(error))
  process.exit(1)
}

const ignored = output
  .split('\n')
  .map((line) => line.trim())
  .filter((line) => line && !line.includes('node_modules'))

if (ignored.length > 0) {
  console.error('[gitignore] source files are ignored by git:')
  for (const file of ignored) {
    console.error(`  - src/${file}`)
  }
  process.exit(1)
}

console.log('[gitignore] ok (no ignored files under src)')
