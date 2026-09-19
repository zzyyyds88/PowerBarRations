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
 * 从"渠道 × 模型 m"加入成员时的默认上游真名：按渠道 model_mapping 以 **m** 为键
 * 解析（有映射取映射右值，无映射取 m 本身）。
 *
 * 查表键是成员所选模型，不是车道名，因此池化车道（车道名 ≠ m）也能命中映射
 * （ADR 0006 §5）。纯函数，便于单测。
 */
export function defaultUpstreamForModel(
  channelName: string,
  model: string,
  catalog: PBRChannelCatalogEntry[]
): string {
  const trimmed = model.trim()
  const channel = catalog.find((c) => c.name === channelName)
  const mapped = channel?.model_mapping?.[trimmed]
  return mapped && mapped.trim() !== '' ? mapped.trim() : trimmed
}
