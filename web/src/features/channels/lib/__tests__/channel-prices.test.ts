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
import { describe, expect, test } from 'vitest'

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  buildSettingJSON,
  type ChannelFormValues,
} from '../channel-form'

// 渠道级上游单价（design-v1 §16#7）的序列化口径。
//
// 后端 `model/log.go estimateRequestCost` 按**请求模型精确匹配**渠道价，命中即用该价、
// **不回退**全局默认单价表。因此若把"只选了模型、没填价格"的空条目也落库，该渠道的
// 折算成本会变成 0，而不是使用全局默认价——这是必须锁住的正确性缺陷。
function build(pbr_prices: unknown[], models: string): Record<string, unknown> {
  const json = buildSettingJSON({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    models,
    pbr_prices,
  } as unknown as ChannelFormValues)
  return JSON.parse(json) as Record<string, unknown>
}

describe('渠道级上游单价序列化', () => {
  test('只落库至少填了一项价格的模型', () => {
    const setting = build(
      [{ model: 'gpt-4o', input: 2.5, output: 10 }, { model: 'gpt-4o-mini' }],
      'gpt-4o,gpt-4o-mini'
    )

    expect(setting.pbr_prices).toEqual([
      {
        model: 'gpt-4o',
        input: 2.5,
        output: 10,
        cache_read: 0,
        cache_write: 0,
      },
    ])
  })

  test('全空价目不得落库（否则成本被算成 0 而非回退全局默认）', () => {
    const setting = build([{ model: 'gpt-4o' }], 'gpt-4o')

    expect(setting.pbr_prices).toBeUndefined()
  })

  test('已从模型清单移除的模型价格会被丢弃', () => {
    const setting = build([{ model: 'removed', input: 1 }], 'kept')

    expect(setting.pbr_prices).toBeUndefined()
  })

  test('显式填 0 视为已配置，仍予保留', () => {
    const setting = build([{ model: 'free', input: 0 }], 'free')

    expect(setting.pbr_prices).toEqual([
      { model: 'free', input: 0, output: 0, cache_read: 0, cache_write: 0 },
    ])
  })
})
