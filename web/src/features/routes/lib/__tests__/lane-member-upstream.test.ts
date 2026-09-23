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
import {
  isModelDeclaredByChannel,
  resolvedUpstreamForMember,
} from '../lane-member-upstream'

// ADR 0008：上游真名 = Channel.ModelMapping[成员所选模型] ?? 成员所选模型。
// 查表键恒为成员所选模型，与车道名（路由键）无关；结果只用于展示，不进写载荷。
const CATALOG: PBRChannelCatalogEntry[] = [
  { name: 'ch-a', enabled: true, models: ['a-1'], model_mapping: {} },
  {
    name: 'ch-b',
    enabled: true,
    models: ['glm-5.3-flash'],
    model_mapping: { 'glm-5.3-flash': 'deepseek-v4.2-flash' },
  },
]

describe('resolvedUpstreamForMember', () => {
  it('returns the mapping target when the selected model is mapped', () => {
    expect(resolvedUpstreamForMember('ch-b', 'glm-5.3-flash', CATALOG)).toBe(
      'deepseek-v4.2-flash'
    )
  })

  it('returns the model name itself when the channel has no mapping for it', () => {
    expect(resolvedUpstreamForMember('ch-a', 'a-1', CATALOG)).toBe('a-1')
    // 映射键不是所选模型时不命中。
    expect(resolvedUpstreamForMember('ch-b', 'other-model', CATALOG)).toBe(
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
    expect(resolvedUpstreamForMember('ch-c', 'm', catalog)).toBe('m')
    expect(resolvedUpstreamForMember('missing', 'm', catalog)).toBe('m')
  })

  // 池化车道（车道名 ≠ 成员所选模型）也必须命中映射：本函数不接收车道名，
  // 因此结果只随渠道映射变化，这正是"改名在渠道配置一次"的兑现。
  it('follows the channel mapping so a mapping edit changes the displayed name', () => {
    const before = resolvedUpstreamForMember('ch-b', 'glm-5.3-flash', CATALOG)
    const after = resolvedUpstreamForMember('ch-b', 'glm-5.3-flash', [
      CATALOG[0],
      {
        name: 'ch-b',
        enabled: true,
        models: ['glm-5.3-flash'],
        model_mapping: { 'glm-5.3-flash': 'deepseek-v4.3-flash' },
      },
    ])
    expect(before).toBe('deepseek-v4.2-flash')
    expect(after).toBe('deepseek-v4.3-flash')
  })
})

describe('isModelDeclaredByChannel', () => {
  it('accepts models declared in the channel model list', () => {
    expect(isModelDeclaredByChannel('ch-a', 'a-1', CATALOG)).toBe(true)
  })

  it('accepts models that are model_mapping keys even when not in the model list', () => {
    const catalog: PBRChannelCatalogEntry[] = [
      {
        name: 'ch-d',
        enabled: true,
        models: [],
        model_mapping: { alias: 'real-upstream' },
      },
    ]
    expect(isModelDeclaredByChannel('ch-d', 'alias', catalog)).toBe(true)
    // 映射的**右值**（上游真名）不是所选模型的合法取值。
    expect(isModelDeclaredByChannel('ch-d', 'real-upstream', catalog)).toBe(
      false
    )
  })

  it('flags models the channel does not declare', () => {
    expect(isModelDeclaredByChannel('ch-a', 'ghost', CATALOG)).toBe(false)
  })

  it('stays silent for unknown channels and blank models', () => {
    // 渠道已删的悬空成员拿不到证据，不得误报（ui-spec §6.3 的告警只做防呆）。
    expect(isModelDeclaredByChannel('missing', 'a-1', CATALOG)).toBe(true)
    expect(isModelDeclaredByChannel('ch-a', '  ', CATALOG)).toBe(true)
  })
})
