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

// ui-spec §7/§8：新增文案必须同时补全部 7 个 locale；漏键会让 i18next 静默回退
// 英文，中文界面出现英文残留。这条测试把"漏键"变成构建期失败。
//
// 只扫描**产品代码**里字面量形态的 t('...') / t("...")：动态键、模板串与测试文件
// 不在此列（它们要么在运行期拼接，要么不是面向用户的文案）。

const LOCALES_DIR = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../locales'
)
const SRC_DIR = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../..'
)

// 允许保持英文的专用名词/标识符（品牌、协议、缩写等）。
const ALLOWLIST = new Set([
  'API Key',
  'API Keys',
  'OpenAI',
  'HTTP',
  'CPU',
  'RAM',
  'JSON',
  'ID',
])

const LITERAL_T_CALL = /\bt\(\s*'([^'\\]{4,})'|\bt\(\s*"([^"\\]{4,})"/g

function loadTranslation(locale: string): Record<string, string> {
  const raw = fs.readFileSync(path.join(LOCALES_DIR, `${locale}.json`), 'utf8')
  return (JSON.parse(raw) as { translation: Record<string, string> })
    .translation
}

function collectSourceFiles(dir: string): string[] {
  const out: string[] = []
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      if (entry.name === 'locales' || entry.name === '__tests__') continue
      out.push(...collectSourceFiles(full))
      continue
    }
    if (!/\.tsx?$/.test(entry.name) || /\.test\.tsx?$/.test(entry.name)) {
      continue
    }
    out.push(full)
  }
  return out
}

/** 收集产品代码里出现的 t('...') 字面量键。 */
function collectLiteralKeys(): Set<string> {
  const keys = new Set<string>()
  for (const file of collectSourceFiles(SRC_DIR)) {
    const text = fs.readFileSync(file, 'utf8')
    for (const match of text.matchAll(LITERAL_T_CALL)) {
      const key = match[1] ?? match[2]
      if (!key) continue
      if (key.startsWith('/') || key.startsWith('#')) continue
      keys.add(key)
    }
  }
  return keys
}

describe('i18n literal key coverage', () => {
  it('every t() literal used in product code exists in zh.json', () => {
    const zh = loadTranslation('zh')
    const missing = [...collectLiteralKeys()].filter(
      (key) => !(key in zh) && !ALLOWLIST.has(key)
    )
    expect(
      missing,
      `以下文案在产品代码里用了 t() 但 zh.json 缺键（会回退英文）：\n${missing
        .sort()
        .map((key) => `  - ${key}`)
        .join('\n')}`
    ).toEqual([])
  })

  it('all locales expose the same literal keys as zh.json', () => {
    const zhKeys = collectLiteralKeys()
    const missingByLocale: Record<string, string[]> = {}
    for (const locale of ['en', 'zh-TW', 'ja', 'fr', 'ru', 'vi']) {
      const table = loadTranslation(locale)
      const missing = [...zhKeys].filter(
        (key) => !(key in table) && !ALLOWLIST.has(key)
      )
      if (missing.length > 0) missingByLocale[locale] = missing.sort()
    }
    expect(missingByLocale).toEqual({})
  })
})
