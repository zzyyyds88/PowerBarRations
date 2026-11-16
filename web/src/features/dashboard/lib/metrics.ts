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
// 首页概览指标条的口径换算（ui-spec §6.2A）。
//
// 五格 = 窗口内的合计（折算成本 / 调用次数 / Token 总量）与每分钟速率
// （RPM / TPM）。速率一律按**窗口分钟数**摊平，短窗口与长窗口口径一致。
//
// 除零是真实存在的输入（异常 from/to、跨度非正），必须收敛成 0：直接相除会得到
// `Infinity` / `NaN`，再经 `toFixed` 或千分位格式化就会把 `Infinity` 显示到界面上。

import type { PBRStatBucket } from '../pbr-stats-api'

/** 窗口分钟数：跨度非正或含非有限值时返回 0（视为"没有可摊平的窗口"）。 */
export function windowMinutes(from: number, to: number): number {
  if (!Number.isFinite(from) || !Number.isFinite(to)) return 0
  const seconds = to - from
  if (!(seconds > 0)) return 0
  return seconds / 60
}

/** 每分钟速率（RPM / TPM）：分钟数 <= 0（空窗口/异常窗口）时返回 0。 */
export function perMinute(total: number, minutes: number): number {
  if (!Number.isFinite(total) || !Number.isFinite(minutes)) return 0
  if (minutes <= 0) return 0
  return total / minutes
}

/** 窗口内的合计（元 / 次 / token）。 */
export interface OverviewWindowTotals {
  /** 折算成本合计（元）。 */
  cost: number
  /** 请求数合计。 */
  requests: number
  /** token 合计 = prompt + completion。 */
  tokens: number
}

/** 把一个窗口的聚合桶摊平成合计：成本 / 请求数 / token。 */
export function sumWindowTotals(items: PBRStatBucket[]): OverviewWindowTotals {
  let cost = 0
  let requests = 0
  let tokens = 0
  for (const item of items) {
    cost += item.estimated_cost || 0
    requests += item.requests || 0
    tokens += (item.prompt_tokens || 0) + (item.completion_tokens || 0)
  }
  return { cost, requests, tokens }
}
