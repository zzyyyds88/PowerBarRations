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
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

import { CHANNEL_PROTOCOL_OPTIONS } from '@/features/channels/constants'

import { HOME_STATS } from '../constants'

const HOME_DIR = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '..'
)

// ui-spec §6.0：默认落地页只陈述 PBR 真实能力。两条回归契约：
// 1) 统计条宣称的协议数必须等于控制台实际暴露的上游协议数，避免文案随实现漂移；
// 2) 落地页文案不得出现上游品牌（ui-spec §2 全量品牌替换）。
describe('home page content contract', () => {
  it('claims the same protocol count as the console actually exposes', () => {
    const protocols = HOME_STATS.find(
      (stat) => stat.labelKey === 'protocols supported'
    )

    expect(protocols?.value).toBe(CHANNEL_PROTOCOL_OPTIONS.length)
    expect(protocols?.value).toBe(4)
  })

  it('does not leak upstream brand names into the landing page', () => {
    const files: string[] = []
    const walk = (dir: string) => {
      for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
        if (entry.name === '__tests__') continue
        const full = path.join(dir, entry.name)
        if (entry.isDirectory()) {
          walk(full)
          continue
        }
        if (/\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name)) {
          files.push(full)
        }
      }
    }
    walk(HOME_DIR)

    const offenders = files
      .filter((file) =>
        /new[\s-]?api|octopus/i.test(fs.readFileSync(file, 'utf8'))
      )
      .map((file) => path.relative(HOME_DIR, file))

    expect(offenders).toEqual([])
  })
})
