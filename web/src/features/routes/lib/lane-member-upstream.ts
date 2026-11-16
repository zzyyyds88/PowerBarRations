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
  const candidates = catalog.filter((c) => c.name === channelName)
  if (candidates.length === 0) return trimmed
  // 渠道名本应唯一；历史重名数据下优先采用真正声明了该模型的条目，
  // 避免拿另一条同名渠道的映射算出错误的上游真名。
  const channel =
    candidates.find(
      (c) =>
        c.models.includes(trimmed) ||
        Object.hasOwn(c.model_mapping ?? {}, trimmed)
    ) ?? candidates[0]
  const mapped = channel.model_mapping?.[trimmed]
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
  const candidates = catalog.filter((c) => c.name === channelName)
  if (candidates.length === 0) return true
  // 同名渠道可能来自历史脏数据：任意一条声明了该模型就不告警，
  // 重名本身由 duplicateChannelNames 的显式告警承担。
  return candidates.some(
    (c) =>
      c.models.includes(trimmed) ||
      Object.hasOwn(c.model_mapping ?? {}, trimmed)
  )
}

/**
 * 返回目录里出现多次的渠道名（历史重名数据）。
 *
 * 渠道名是管理面与成员的唯一寻址键；重名会让"保存成员"无法确定目标。
 * 控制台据此显示显式告警并阻止保存含重名渠道的成员，而不是静默取第一条。
 */
export function duplicateChannelNames(
  catalog: PBRChannelCatalogEntry[]
): string[] {
  const counts = new Map<string, number>()
  for (const entry of catalog) {
    counts.set(entry.name, (counts.get(entry.name) ?? 0) + 1)
  }
  return [...counts.entries()]
    .filter(([, count]) => count > 1)
    .map(([name]) => name)
}
