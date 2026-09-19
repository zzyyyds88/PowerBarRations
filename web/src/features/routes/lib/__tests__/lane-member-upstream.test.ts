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

import type { PBRChannelCatalogEntry } from '../../api'
import { defaultUpstreamForModel } from '../lane-member-upstream'

// ADR 0006 §5：加入成员时以「成员所选模型 m」为键查渠道映射，与车道名无关。
const CATALOG: PBRChannelCatalogEntry[] = [
  { name: 'ch-a', enabled: true, models: ['a-1'], model_mapping: {} },
  {
    name: 'ch-b',
    enabled: true,
    models: ['glm-5.3-flash'],
    model_mapping: { 'glm-5.3-flash': 'deepseek-v4.2-flash' },
  },
]

describe('defaultUpstreamForModel', () => {
  it('returns the mapping target when the selected model is mapped', () => {
    expect(defaultUpstreamForModel('ch-b', 'glm-5.3-flash', CATALOG)).toBe(
      'deepseek-v4.2-flash'
    )
  })

  it('returns the model name itself when the channel has no mapping for it', () => {
    expect(defaultUpstreamForModel('ch-a', 'a-1', CATALOG)).toBe('a-1')
    // 映射键不是所选模型时不命中。
    expect(defaultUpstreamForModel('ch-b', 'other-model', CATALOG)).toBe(
      'other-model'
    )
  })

  it('ignores blank mapping targets and unknown channels', () => {
    const catalog: PBRChannelCatalogEntry[] = [
      {
        name: 'ch-c',
        enabled: true,
        models: ['m'],
        model_mapping: { m: '   ' },
      },
    ]
    expect(defaultUpstreamForModel('ch-c', 'm', catalog)).toBe('m')
    expect(defaultUpstreamForModel('missing', 'm', catalog)).toBe('m')
  })
})
