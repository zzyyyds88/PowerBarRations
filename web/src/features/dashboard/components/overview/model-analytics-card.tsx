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
// 首页概览「模型数据分析」卡（ui-spec §6.2A 第 1 块，左 2/3）。
//
// 胶囊 tab（调用趋势 / 消耗分布 / 调用次数分布 / 调用次数排行）+ 柱状图；
// 标题右侧挂当前窗口的主指标汇总（消耗 tab 是金额、其余 tab 是次数），随 tab 变。
// 窗口级指标（折算成本 / 调用次数 / Token 总量 / RPM / TPM）已提到面板级指标条，
// 本卡不再重复渲染。
//
// 数据源是 GET /api/stats（聚合表，长期保留、不受日志清理影响）：
// - 汇总恒按当前窗口统计，切 tab 不换窗口；
// - 「调用次数分布」按 group_by=channel 回答"流量落在哪个渠道"，其余 tab 按 group_by=model；
//   粒度由区间跨度决定（> 2 天用 day）。
// - 查询键与指标条共用（见 lib/overview-query），同一窗口只发一次请求。

import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatCostYuan } from '@/features/keys/lib/cost'

import { formatCount } from '../../lib/format'
import { overviewStatsQueryKey } from '../../lib/overview-query'
import {
  buildBucketAxis,
  type PBRResolvedTimeRange,
  type PBRTimeRangeGranularity,
} from '../../lib/time-range'
import {
  getPBRStats,
  type PBRStatBucket,
  type PBRStatsGroupBy,
} from '../../pbr-stats-api'
import { PanelTotal } from '../ui/panel-total'
import { PanelWrapper } from '../ui/panel-wrapper'
import { OverviewBarChart, type OverviewBarChartDatum } from './bar-chart'

type AnalyticsTab = 'cost' | 'trend' | 'requests' | 'ranking'
type AnalyticsMetric = 'cost' | 'requests'

/** tab 顺序向外部看板对齐：趋势 → 分布 → 排行（默认第一个）。 */
const TAB_VALUES: AnalyticsTab[] = ['trend', 'cost', 'requests', 'ranking']
/** 每个 tab 的维度与主指标：不同 tab 取不同 group_by（api-spec §5.5）。 */
const TAB_DIMENSIONS: Record<
  AnalyticsTab,
  { dimension: PBRStatsGroupBy; metric: AnalyticsMetric }
> = {
  cost: { dimension: 'model', metric: 'cost' },
  trend: { dimension: 'model', metric: 'requests' },
  requests: { dimension: 'channel', metric: 'requests' },
  ranking: { dimension: 'model', metric: 'requests' },
}

const TOP_GROUP_LIMIT = 10
const EMPTY_BUCKETS: PBRStatBucket[] = []
const STALE_TIME_MS = 60 * 1000

function sumRequests(items: PBRStatBucket[]): number {
  let requests = 0
  for (const item of items) requests += item.requests || 0
  return requests
}

function sumCost(items: PBRStatBucket[]): number {
  let cost = 0
  for (const item of items) cost += item.estimated_cost || 0
  return cost
}

function formatBucketLabel(
  ts: number,
  granularity: PBRTimeRangeGranularity
): string {
  const date = new Date(ts * 1000)
  if (granularity === 'day') {
    return date.toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric',
    })
  }
  return date.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
  })
}

export function ModelAnalyticsCard(props: { range: PBRResolvedTimeRange }) {
  const { t } = useTranslation()
  // 默认 tab = 趋势（对外部看板的 tab 顺序：趋势 → 分布 → 排行）。
  const [tab, setTab] = useState<AnalyticsTab>('trend')
  const range = props.range
  const usesChannel = TAB_DIMENSIONS[tab].dimension === 'channel'

  // 当前窗口（按模型）：趋势图、排行与标题汇总的主数据源。
  // 键与指标条共用同一份（见 lib/overview-query），同一窗口只发一次请求。
  const modelQuery = useQuery({
    queryKey: overviewStatsQueryKey('model', range),
    queryFn: () =>
      getPBRStats({
        granularity: range.granularity,
        from: range.from,
        to: range.to,
        groupBy: 'model',
      }),
    staleTime: STALE_TIME_MS,
    refetchOnWindowFocus: false,
  })

  // 渠道维度：只有「调用次数分布」需要。
  const channelQuery = useQuery({
    queryKey: overviewStatsQueryKey('channel', range),
    enabled: usesChannel,
    queryFn: () =>
      getPBRStats({
        granularity: range.granularity,
        from: range.from,
        to: range.to,
        groupBy: 'channel',
      }),
    staleTime: STALE_TIME_MS,
    refetchOnWindowFocus: false,
  })

  const modelItems = modelQuery.data?.items ?? EMPTY_BUCKETS
  const channelItems = channelQuery.data?.items ?? EMPTY_BUCKETS
  const activeItems = usesChannel ? channelItems : modelItems

  const activeConfig = TAB_DIMENSIONS[tab]
  // 标题汇总跟随当前 tab 的主指标：消耗 tab 给金额，其余 tab 给次数。
  // 数据未到位（加载中/失败）时为 null，标题右侧留空——不显示伪造的 `Total: 0`。
  let headerTotal: string | null = null
  if (activeItems.length > 0) {
    headerTotal =
      activeConfig.metric === 'cost'
        ? formatCostYuan(sumCost(activeItems))
        : formatCount(sumRequests(activeItems))
  }
  const chartData = useMemo((): OverviewBarChartDatum[] => {
    if (tab === 'trend') {
      const step = range.granularity === 'day' ? 86400 : 3600
      const byBucket = new Map<number, number>()
      for (const item of modelItems) {
        const bucket = Math.floor(item.bucket_ts / step) * step
        byBucket.set(bucket, (byBucket.get(bucket) ?? 0) + (item.requests || 0))
      }
      return buildBucketAxis(range.from, range.to, range.granularity).map(
        (ts) => ({
          key: String(ts),
          label: formatBucketLabel(ts, range.granularity),
          value: byBucket.get(ts) ?? 0,
        })
      )
    }
    const byGroup = new Map<string, number>()
    for (const item of activeItems) {
      if (!item.group) continue
      const value =
        activeConfig.metric === 'cost'
          ? item.estimated_cost || 0
          : item.requests || 0
      byGroup.set(item.group, (byGroup.get(item.group) ?? 0) + value)
    }
    return [...byGroup.entries()]
      .map(([group, value]) => ({ key: group, label: group, value }))
      .sort((left, right) => right.value - left.value)
      .slice(0, TOP_GROUP_LIMIT)
  }, [
    tab,
    modelItems,
    activeItems,
    activeConfig.metric,
    range.from,
    range.to,
    range.granularity,
  ])

  const valueFormatter =
    tab === 'cost' ? formatCostYuan : (value: number) => formatCount(value)

  const isLoading =
    modelQuery.isLoading || (usesChannel && channelQuery.isLoading)
  const isError = modelQuery.isError || (usesChannel && channelQuery.isError)
  const isEmpty = !isLoading && !isError && activeItems.length === 0

  const handleRetry = () => {
    void modelQuery.refetch()
    if (usesChannel) void channelQuery.refetch()
  }

  const tabLabels: Record<AnalyticsTab, string> = {
    cost: t('Consumption distribution'),
    trend: t('Call trend'),
    requests: t('Call count distribution'),
    ranking: t('Call count ranking'),
  }

  let body = (
    <OverviewBarChart
      data={chartData}
      valueFormatter={valueFormatter}
      ariaLabel={tabLabels[tab]}
    />
  )
  if (isError) {
    body = (
      <ErrorState
        className='min-h-[200px]'
        title={t('Failed to load')}
        onRetry={handleRetry}
      />
    )
  } else if (isEmpty) {
    body = (
      <p className='text-muted-foreground flex min-h-[200px] items-center justify-center text-center text-sm'>
        {t('No data in the selected range')}
      </p>
    )
  } else if (tab === 'ranking') {
    body = <RankingList data={chartData} valueFormatter={valueFormatter} />
  }

  return (
    <PanelWrapper
      title={t('Model analytics')}
      loading={isLoading}
      height='h-[320px]'
      headerActions={<PanelTotal value={headerTotal} />}
    >
      <div className='flex flex-col gap-3'>
        <Tabs
          value={tab}
          onValueChange={(value) => setTab(String(value) as AnalyticsTab)}
        >
          <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
            {TAB_VALUES.map((value) => (
              <TabsTrigger key={value} value={value}>
                {tabLabels[value]}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        {body}
      </div>
    </PanelWrapper>
  )
}

/** 调用次数排行：逐模型一行（名次 / 模型名 / 占比条 / 次数）。 */
function RankingList(props: {
  data: OverviewBarChartDatum[]
  valueFormatter: (value: number) => string
}) {
  const { t } = useTranslation()
  let max = 0
  for (const datum of props.data) max = Math.max(max, datum.value)

  return (
    <ol className='flex flex-col gap-1.5' aria-label={t('Call count ranking')}>
      {props.data.map((datum, index) => {
        const ratio = max > 0 ? datum.value / max : 0
        return (
          <li
            key={datum.key}
            className='flex items-center gap-2'
            title={`${datum.label} · ${props.valueFormatter(datum.value)}`}
          >
            <span className='text-muted-foreground w-5 shrink-0 text-right font-mono text-xs tabular-nums'>
              {index + 1}
            </span>
            <span className='w-32 shrink-0 truncate font-mono text-xs sm:w-44'>
              {datum.label}
            </span>
            <span className='bg-muted h-2 min-w-0 flex-1 overflow-hidden rounded-full'>
              <span
                className='bg-success/70 block h-full rounded-full'
                style={{ width: `${Math.max(2, ratio * 100)}%` }}
              />
            </span>
            <span className='w-16 shrink-0 text-right font-mono text-xs tabular-nums'>
              {props.valueFormatter(datum.value)}
            </span>
          </li>
        )
      })}
    </ol>
  )
}
