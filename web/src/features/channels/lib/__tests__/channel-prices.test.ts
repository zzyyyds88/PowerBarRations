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
  channelFormSchema,
  type ChannelFormValues,
} from '../channel-form'

// 渠道级上游单价（design-v1 §16.9#7，单层单价）的序列化口径。
//
// 后端 `model/log.go estimateRequestCost` 按**请求模型精确匹配**渠道价，命中即用该价、
// 不命中即不折算（0），没有全局默认单价表。因此若把"只选了模型、没填价格"的空条目也
// 落库，该模型的折算成本会变成 0，而不是保持"未配置"——这是必须锁住的正确性缺陷。
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

  test('全空价目不得落库（否则该模型成本被算成 0 而非"未配置"）', () => {
    const setting = build([{ model: 'gpt-4o' }], 'gpt-4o')

    expect(setting.pbr_prices).toBeUndefined()
  })

  test('清单外的自定义计价行（独立车道名等）保留', () => {
    // 计价键是请求模型名，允许是清单之外的行（如车道成员引用的上游名/独立车道名）。
    const setting = build([{ model: 'my-lane', input: 1 }], 'kept')

    expect(setting.pbr_prices).toEqual([
      { model: 'my-lane', input: 1, output: 0, cache_read: 0, cache_write: 0 },
    ])
  })

  test('显式填 0 视为已配置，仍予保留', () => {
    const setting = build([{ model: 'free', input: 0 }], 'free')

    expect(setting.pbr_prices).toEqual([
      { model: 'free', input: 0, output: 0, cache_read: 0, cache_write: 0 },
    ])
  })
})

describe('渠道级上游单价的峰谷（ui-spec §9）', () => {
  test('旧单层价落库形状不变：不补 timezone/bands', () => {
    const setting = build([{ model: 'gpt-4o', input: 2.5 }], 'gpt-4o')
    const prices = setting.pbr_prices as Record<string, unknown>[]

    expect(prices).toEqual([
      { model: 'gpt-4o', input: 2.5, output: 0, cache_read: 0, cache_write: 0 },
    ])
    expect(prices[0]).not.toHaveProperty('bands')
    expect(prices[0]).not.toHaveProperty('timezone')
  })

  test('timezone 与 bands 随价格行落库，留空的 band 价不冒充 0', () => {
    const setting = build(
      [
        {
          model: 'gpt-4o',
          timezone: 'Asia/Shanghai',
          bands: [
            { days: [1, 2, 3], start: '00:30', end: '08:30', input: 5 },
            { days: [1, 2, 3], start: '08:30', end: '00:30' },
          ],
        },
      ],
      'gpt-4o'
    )

    expect(setting.pbr_prices).toEqual([
      {
        model: 'gpt-4o',
        input: 0,
        output: 0,
        cache_read: 0,
        cache_write: 0,
        timezone: 'Asia/Shanghai',
        bands: [
          { days: [1, 2, 3], start: '00:30', end: '08:30', input: 5 },
          { days: [1, 2, 3], start: '08:30', end: '00:30' },
        ],
      },
    ])
  })

  test('表单校验（zod）不得剥掉峰谷字段', () => {
    const parsed = channelFormSchema.parse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'channel-a',
      models: 'gpt-4o',
      pbr_prices: [
        {
          model: 'gpt-4o',
          timezone: 'UTC',
          bands: [{ days: [1], start: '00:30', end: '08:30' }],
        },
      ],
    })

    expect(parsed.pbr_prices).toEqual([
      {
        model: 'gpt-4o',
        timezone: 'UTC',
        bands: [{ days: [1], start: '00:30', end: '08:30' }],
      },
    ])
  })
})
