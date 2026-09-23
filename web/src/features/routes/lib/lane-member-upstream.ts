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
import type { PBRChannelCatalogEntry } from '../api'

/**
 * 成员「解析后的上游」真名，**纯展示用**派生值（ADR 0008）。
 *
 * 规则与服务端一致且只有一条：`渠道 model_mapping[成员所选模型] ?? 成员所选模型`。
 * 查表键恒为**成员所选模型**，与车道名（路由键）无关——池化车道（车道名 ≠ 模型名）
 * 下也正确。渠道未知或无映射时回落到模型名本身。
 *
 * **这不是"加入成员时写入的默认值"**：成员表只存所选模型，真名一律由服务端推导，
 * 因此本函数的结果**不进任何写载荷**，只用于成员行与卡片的只读展示。
 */
export function resolvedUpstreamForMember(
  channelName: string,
  model: string,
  catalog: PBRChannelCatalogEntry[]
): string {
  const trimmed = model.trim()
  const channel = catalog.find((c) => c.name === channelName)
  const mapped = channel?.model_mapping?.[trimmed]
  return mapped && mapped.trim() !== '' ? mapped.trim() : trimmed
}

/**
 * 该渠道是否声明了成员所选模型（渠道 `models` 清单，或作为 `model_mapping` 的左键）。
 *
 * 用于成员行的防呆告警（ui-spec §6.3）：未声明时提示"渠道未声明该模型"，
 * **不拦截保存**。渠道不在目录里（悬空成员）返回 `true`——拿不到证据就不告警，
 * 否则渠道被删的历史成员会被误报。
 */
export function isModelDeclaredByChannel(
  channelName: string,
  model: string,
  catalog: PBRChannelCatalogEntry[]
): boolean {
  const trimmed = model.trim()
  if (trimmed === '') return true
  const channel = catalog.find((c) => c.name === channelName)
  if (!channel) return true
  if (channel.models.includes(trimmed)) return true
  return Object.hasOwn(channel.model_mapping ?? {}, trimmed)
}
