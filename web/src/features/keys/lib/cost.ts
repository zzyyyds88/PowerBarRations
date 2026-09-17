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
// 令牌「消耗」金额展示（ui-spec §6.5 / token-spec §3.7）。
// cost 的单位是元（与看板/日志的 estimated_cost 同口径），与看板 formatCost 一致：
// 0 显示 ¥0，小额保留 4 位小数，其余两位。

/** 把「元」金额格式化为展示字符串；非法值按 0 处理。 */
export function formatCostYuan(value: number | null | undefined): string {
  const amount = typeof value === 'number' && Number.isFinite(value) ? value : 0
  if (amount === 0) return '¥0'
  if (Math.abs(amount) < 0.01) return `¥${amount.toFixed(4)}`
  return `¥${amount.toFixed(2)}`
}
