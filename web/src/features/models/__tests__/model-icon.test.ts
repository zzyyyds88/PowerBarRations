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
import { describe, expect, it } from 'vitest'

import { resolveModelIconKey } from '../lib/model-icon'

describe('resolveModelIconKey', () => {
  it('prefers an explicitly configured icon', () => {
    expect(
      resolveModelIconKey({ model_name: 'deepseek-v4-flash', icon: 'OpenAI' })
    ).toBe('OpenAI')
  })

  it('ignores a whitespace-only explicit icon and infers from the model name', () => {
    expect(
      resolveModelIconKey({ model_name: 'deepseek-v4-flash', icon: '   ' })
    ).toBe('DeepSeek.Color')
  })

  it.each([
    ['glm-5.2', 'Zhipu.Color'],
    ['deepseek-v4-flash', 'DeepSeek.Color'],
    ['bge-m3', 'BAAI'],
    ['claude-sonnet-4', 'Claude.Color'],
    ['gpt-5', 'OpenAI.Color'],
  ])('infers the known provider icon for %s', (modelName, expected) => {
    expect(resolveModelIconKey({ model_name: modelName })).toBe(expected)
  })

  it('falls back to the first character for an unknown model', () => {
    expect(resolveModelIconKey({ model_name: 'mystery-model' })).toBe('m')
  })

  it('falls back to an empty string when the model name is empty', () => {
    expect(resolveModelIconKey({ model_name: '', icon: '' })).toBe('')
  })

  it('trims the model name before taking the fallback character', () => {
    expect(resolveModelIconKey({ model_name: '  9-unknown' })).toBe('9')
  })
})
