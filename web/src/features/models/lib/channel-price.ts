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
  parseModelsList,
} from '@/features/channels/lib'
import type { Channel } from '@/features/channels/types'

// 模型 ↔ 渠道声明匹配工具（ui-spec §6.3）。
//
// 一个模型可能由多个渠道提供；这些函数负责"某渠道是否声明该模型"
// （models 清单或 model_mapping 映射），供模型抽屉的渠道关联段使用。
// 计价只在渠道编辑「上游单价」页签（ChannelPricesEditor），此处不再涉及。

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
