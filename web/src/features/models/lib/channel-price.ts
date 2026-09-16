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
import {
  extractMappingSourceModels,
  parseChannelSettings,
  parseModelsList,
} from '@/features/channels/lib'
import type { Channel, ChannelModelPrice } from '@/features/channels/types'

// 渠道级上游单价的匹配工具（design-v1 §16#7）。
//
// 一个模型可能由多个渠道提供，各自采购价不同；这些函数负责"某渠道是否服务该模型"
// 与"该渠道为它配了什么价"，供模型页的有效单价列与模型抽屉的渠道关联段共用。

/** 模型名按 name_rule 匹配：0 精确 / 1 前缀 / 2 包含 / 3 后缀。 */
export function matchesName(
  name: string,
  modelName: string,
  rule: number
): boolean {
  switch (rule) {
    case 1:
      return name.startsWith(modelName)
    case 2:
      return name.includes(modelName)
    case 3:
      return name.endsWith(modelName)
    default:
      return name === modelName
  }
}

/** 渠道可路由的键：models 声明 + model_mapping 源键。 */
export function channelRouteKeys(channel: Channel): string[] {
  const keys = [
    ...parseModelsList(channel.models),
    ...extractMappingSourceModels(channel.model_mapping ?? ''),
  ]
  return [...new Set(keys)]
}

/** 渠道为该模型配置的上游单价；未配置返回 undefined。 */
export function findChannelPrice(
  channel: Channel,
  modelName: string,
  rule: number
): ChannelModelPrice | undefined {
  const prices = parseChannelSettings(channel.setting).pbr_prices ?? []
  if (rule === 0) return prices.find((item) => item.model === modelName)
  return prices.find((item) => matchesName(item.model, modelName, rule))
}
