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
// 首页概览的聚合查询键（ui-spec §6.2A）。
//
// 指标条与「模型数据分析」卡看的是同一个窗口、同一个 `group_by=model` 的聚合，
// 两处必须**字面量一致**才能让 react-query 去重成一次请求（键不一致 = 悄悄发两次）。
// 因此键只在这里构造，调用方不再各自拼数组。

import type { PBRStatsGroupBy } from '../pbr-stats-api'
import type { PBRResolvedTimeRange } from './time-range'

/** 当前窗口的聚合查询键。 */
export function overviewStatsQueryKey(
  groupBy: PBRStatsGroupBy,
  range: Pick<PBRResolvedTimeRange, 'from' | 'to' | 'granularity'>
) {
  return [
    'dashboard',
    'overview',
    'stats',
    groupBy,
    range.from,
    range.to,
    range.granularity,
  ] as const
}

/** 上一等长窗口（环比基准）的聚合查询键。 */
export function overviewPreviousStatsQueryKey(
  groupBy: PBRStatsGroupBy,
  range: PBRResolvedTimeRange
) {
  return [
    'dashboard',
    'overview',
    'stats-previous',
    groupBy,
    range.previousFrom,
    range.previousTo,
    range.granularity,
  ] as const
}
