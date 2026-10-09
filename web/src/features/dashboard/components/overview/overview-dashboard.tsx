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
// 首页概览面板（ui-spec §6.2A）。
//
// 顶部是整页共用的时间分档选择器，换算成 from/to（Unix 秒）后驱动下面三块卡片：
// 模型数据分析（左 2/3）+ Token 排行榜（右 1/3），下一行左半是服务状态。
// 卡片自适应高度、内容不被裁切；长列表在卡内滚动，不做整页横向滚动。

import { useMemo, useState } from 'react'

import {
  resolveTimeRange,
  type PBRTimeRangeSelection,
} from '../../lib/time-range'
import { ModelAnalyticsCard } from './model-analytics-card'
import { ServiceStatusCard } from './service-status-card'
import { TimeRangeSelector } from './time-range-selector'
import { TokenRankingCard } from './token-ranking-card'

const DEFAULT_SELECTION: PBRTimeRangeSelection = { preset: '24h' }

export function OverviewDashboard() {
  const [selection, setSelection] =
    useState<PBRTimeRangeSelection>(DEFAULT_SELECTION)
  // 档位不变时窗口保持稳定（查询键不抖动）；换档才按新的当前时刻重算。
  const range = useMemo(() => resolveTimeRange(selection), [selection])

  return (
    <div className='flex flex-col gap-(--density-grid)'>
      <TimeRangeSelector selection={selection} onChange={setSelection} />
      <div className='grid grid-cols-1 gap-(--density-grid) lg:grid-cols-6'>
        <div className='lg:col-span-4'>
          <ModelAnalyticsCard range={range} />
        </div>
        <div className='lg:col-span-2'>
          <TokenRankingCard range={range} />
        </div>
        <div className='lg:col-span-3'>
          <ServiceStatusCard range={range} />
        </div>
      </div>
    </div>
  )
}
