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
import { resolveModelProvider } from '@/lib/model-provider'

import type { Model } from '../types'

// 模型图标推断（ui-spec §6.3）：显式 model.icon > 按模型名推断的厂商图标 > 模型名首字符。
// 推断只用于只读展示，不写库；编辑弹窗据此展示「识别到的图标」并允许一键采用。

type ModelIconSource = Pick<Model, 'model_name' | 'icon'>

/**
 * 解析模型应渲染的 lobe 图标键。
 *
 * 取值顺序：显式 icon（去除首尾空白后非空）→ resolveModelProvider(model_name).icon
 * → model_name 首字符（未知模型兜底）。
 */
export function resolveModelIconKey(model: ModelIconSource): string {
  const explicit = model.icon?.trim()
  if (explicit) return explicit

  const provider = resolveModelProvider(model.model_name ?? '')
  if (provider && provider.icon) return provider.icon

  return model.model_name?.trim()[0] ?? ''
}
