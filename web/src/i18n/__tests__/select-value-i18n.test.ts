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

// 守卫：Base UI 的 <SelectValue /> 在 Select 根**没有 items** 且自身**没有 children**
// 时，会回显选中项的原始 value（如 "channel"、"failover"、"scheduled_all"），
// 绕过 i18n，中文界面出现英文原始值。
//
// 正确的两种写法：
//   a) 给 Select 根传 items（推荐；Base UI 据此解析 label）
//   b) 给 SelectValue 传 children（如 {t(mode)}）
//
// 本测试扫描所有 .tsx，找出"根 Select 既无 items、SelectValue 又自闭合"的写法，
// 把这类 i18n 回退变成构建期失败。

const SRC_DIR = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../..'
)

/** 递归收集产品代码（排除测试与 locale）。 */
function collectSourceFiles(dir: string): string[] {
  const out: string[] = []
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      if (
        entry.name === 'node_modules' ||
        entry.name === 'locales' ||
        entry.name === '__tests__'
      ) {
        continue
      }
      out.push(...collectSourceFiles(full))
      continue
    }
    if (!entry.name.endsWith('.tsx') || entry.name.endsWith('.test.tsx')) {
      continue
    }
    out.push(full)
  }
  return out
}

/** 从 start 起找到当前 JSX 开标签的 '>'（跳过字符串、花括号、嵌套标签）。 */
function tagEnd(src: string, start: number): number {
  let depth = 0
  let i = start
  let quote: string | null = null
  while (i < src.length) {
    const c = src[i]
    if (quote) {
      if (c === quote && src[i - 1] !== '\\') quote = null
    } else if (c === '"' || c === "'") {
      quote = c
    } else if (c === '{') {
      depth += 1
    } else if (c === '}') {
      depth -= 1
    } else if (c === '>' && depth === 0) {
      return i + 1
    }
    i += 1
  }
  return src.length
}

/** 找出文件里所有"根 Select"开标签的范围（排除 SelectTrigger/Content/... 等子组件）。 */
function rootSelectRanges(src: string): [number, number][] {
  const re = /<Select(?![A-Za-z])/g
  const ranges: [number, number][] = []
  let m: RegExpExecArray | null
  while ((m = re.exec(src)) !== null) {
    ranges.push([m.index, tagEnd(src, m.index)])
  }
  return ranges
}

describe('Select i18n 守卫', () => {
  it('自闭合的 SelectValue 必须位于带 items 的 Select 根内', () => {
    const offenders: string[] = []

    for (const file of collectSourceFiles(SRC_DIR)) {
      const src = fs.readFileSync(file, 'utf8')
      const roots = rootSelectRanges(src)

      for (const match of src.matchAll(/<SelectValue\s*\/>/g)) {
        const idx = match.index ?? 0
        // 包含该 SelectValue 的最近的、且在其之前的根 Select。
        let enclosing: [number, number] | null = null
        for (const range of roots) {
          if (range[0] < idx) enclosing = range
          else break
        }
        const line = src.slice(0, idx).split('\n').length
        const rel = path.relative(SRC_DIR, file)

        if (!enclosing) {
          offenders.push(`${rel}:${line} 找不到所属的 Select 根`)
          continue
        }
        const rootTag = src.slice(enclosing[0], enclosing[1])
        if (!/\bitems=/.test(rootTag)) {
          offenders.push(
            `${rel}:${line} 自闭合 <SelectValue /> 但所属 Select 根没有 items（会回显原始值，绕过 i18n）`
          )
        }
      }
    }

    expect(
      offenders,
      `以下 Select 会回显原始值而非翻译标签，请给 Select 根加 items 或给 SelectValue 加 children：\n${offenders
        .map((o) => `  - ${o}`)
        .join('\n')}`
    ).toEqual([])
  })
})
