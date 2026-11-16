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
import { lazy, Suspense, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConsolePage } from '@/components/layout'
import { Skeleton } from '@/components/ui/skeleton'

import { TimeRangeSelector } from './components/overview/time-range-selector'
import { resolveTimeRange, type PBRTimeRangeSelection } from './lib/time-range'

// 数据看板（ui-spec §6.2）：统一时间区间驱动全部面板。
// 顶部 = 模型健康（合并成本/Token 摘要、服务状态表与实时吞吐）；
// 再下 = 模型数据分析（全宽）；
// 最后 = 分组统计表（全宽，按调用次数 / 上游花费 / Token 排序）。
// 实时路由已独立为 /live-routing，概览不再承载拓扑。
// 服务状态已并入模型健康，不再单独占卡；Token 排行榜已并入统计表。
// 路由壳 /dashboard/$section 保留（URL 兼容）。

const LazyModelHealthPanel = lazy(() =>
  import('./components/overview/model-health-panel').then((m) => ({
    default: m.ModelHealthPanel,
  }))
)
const LazyModelAnalyticsCard = lazy(() =>
  import('./components/overview/model-analytics-card').then((m) => ({
    default: m.ModelAnalyticsCard,
  }))
)
const LazyUsageInsightsCard = lazy(() =>
  import('./components/overview/usage-insights-card').then((m) => ({
    default: m.UsageInsightsCard,
  }))
)
const LazyStatsTableCard = lazy(() =>
  import('./components/cost/cost-dashboard').then((m) => ({
    default: m.StatsTableCard,
  }))
)

function CardSkeleton(props: { className?: string }) {
  return (
    <Skeleton className={`rounded-panel h-[320px] ${props.className ?? ''}`} />
  )
}

export function Dashboard(
  props: {
    selection?: PBRTimeRangeSelection
    onSelectionChange?: (selection: PBRTimeRangeSelection) => void
  } = {}
) {
  const { t } = useTranslation()
  const [localSelection, setLocalSelection] = useState<PBRTimeRangeSelection>({
    preset: '24h',
  })
  const selection = props.selection ?? localSelection
  const setSelection = (next: PBRTimeRangeSelection) => {
    setLocalSelection(next)
    props.onSelectionChange?.(next)
  }
  // 档位不变时窗口保持稳定（查询键不抖动）；换档才按新的当前时刻重算。
  const range = useMemo(() => resolveTimeRange(selection), [selection])

  return (
    <ConsolePage
      title={t('Overview')}
      description={t('Health, throughput and cost at a glance.')}
    >
      <div className='space-y-(--density-grid)'>
        {/* 时间区间选择器在「数据看板」标题正下方（用户指定，2026-10-26）。 */}
        <TimeRangeSelector selection={selection} onChange={setSelection} />
        {/* 模型健康：成本/Token 摘要 + 服务状态表 + 实时吞吐，单一面板。 */}
        <Suspense fallback={<Skeleton className='rounded-panel h-[320px]' />}>
          <LazyModelHealthPanel range={range} />
        </Suspense>
        {/* 模型数据分析：全宽（服务状态已并入模型健康）。 */}
        <Suspense fallback={<CardSkeleton />}>
          <LazyModelAnalyticsCard range={range} />
        </Suspense>
        {/* 用量洞察：缓存命中率、客户端分布与 53 周日历（读聚合表）。 */}
        <Suspense fallback={<CardSkeleton />}>
          <LazyUsageInsightsCard range={range} />
        </Suspense>
        {/* 行 2：分组统计表（全宽；Token 排序已并入）。 */}
        <Suspense fallback={<Skeleton className='rounded-panel h-[360px]' />}>
          <LazyStatsTableCard range={range} />
        </Suspense>
      </div>
    </ConsolePage>
  )
}
