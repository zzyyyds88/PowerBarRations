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
// 首页概览「指标条」（ui-spec §6.2A）：时间分档选择器下面、三块卡之上的一行五格。
//
// 五格 = 折算成本 / 调用次数 / Token 总量 / RPM / TPM，全部是**当前窗口**的口径，
// 每格带与上一等长窗口的环比。RPM、TPM 用窗口分钟数摊平（见 lib/metrics）。
//
// 数据源与「模型数据分析」卡同一个 `GET /api/stats?group_by=model` 查询
// （共用 lib/overview-query 的查询键，react-query 自动去重成一次请求）。

import {
  Coins01Icon,
  CubeIcon,
  DashboardSpeed01Icon,
  FlashIcon,
  HashtagIcon,
} from '@hugeicons/core-free-icons'
import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Skeleton } from '@/components/ui/skeleton'
import { formatCostYuan } from '@/features/keys/lib/cost'

import { deltaPercent, formatCount, formatTokenAmount } from '../../lib/format'
import { perMinute, sumWindowTotals, windowMinutes } from '../../lib/metrics'
import {
  overviewPreviousStatsQueryKey,
  overviewStatsQueryKey,
} from '../../lib/overview-query'
import {
  formatRequestsPerMinute,
  formatTokensPerMinute,
} from '../../lib/rate-format'
import type { PBRResolvedTimeRange } from '../../lib/time-range'
import {
  getPBRStats,
  type PBRStatBucket,
  type PBRStatsGroupBy,
} from '../../pbr-stats-api'
import { MetricTile } from './metric-tile'

const EMPTY_BUCKETS: PBRStatBucket[] = []
const STALE_TIME_MS = 60 * 1000
/** 指标条与「模型数据分析」卡共用模型维度，保证两侧数字同源。 */
const WINDOW_GROUP_BY: PBRStatsGroupBy = 'model'
/** 五格：窄屏一列、sm 两列、lg 五列（窄屏换行而不是横向挤）。 */
const GRID_CLASS =
  'grid grid-cols-1 gap-(--density-grid) sm:grid-cols-2 lg:grid-cols-5'
/** 五格的稳定标识（骨架占位与 React key 都用它，不用数组下标）。 */
const CELL_IDS = ['cost', 'requests', 'tokens', 'rpm', 'tpm'] as const
/** 五格各自的图标（装饰性；语义由标签承担）。 */
const CELL_ICONS = [
  Coins01Icon,
  HashtagIcon,
  CubeIcon,
  DashboardSpeed01Icon,
  FlashIcon,
]

export function MetricStrip(props: { range: PBRResolvedTimeRange }) {
  const { t, i18n } = useTranslation()
  const range = props.range

  const query = useQuery({
    queryKey: overviewStatsQueryKey(WINDOW_GROUP_BY, range),
    queryFn: () =>
      getPBRStats({
        granularity: range.granularity,
        from: range.from,
        to: range.to,
        groupBy: WINDOW_GROUP_BY,
      }),
    staleTime: STALE_TIME_MS,
    refetchOnWindowFocus: false,
  })

  // 上一等长窗口：五格的环比基准。
  const previousQuery = useQuery({
    queryKey: overviewPreviousStatsQueryKey(WINDOW_GROUP_BY, range),
    queryFn: () =>
      getPBRStats({
        granularity: range.granularity,
        from: range.previousFrom,
        to: range.previousTo,
        groupBy: WINDOW_GROUP_BY,
      }),
    staleTime: STALE_TIME_MS,
    refetchOnWindowFocus: false,
  })

  const items = query.data?.items ?? EMPTY_BUCKETS
  const previousItems = previousQuery.data?.items ?? EMPTY_BUCKETS
  const totals = useMemo(() => sumWindowTotals(items), [items])
  const previousTotals = useMemo(
    () => sumWindowTotals(previousItems),
    [previousItems]
  )

  // 速率一律按各自窗口的分钟数摊平；窗口跨度为 0（异常 from/to）时收敛成 0，
  // 不出现 NaN/Infinity。环比基准的窗口与当前窗口等长（resolveTimeRange 保证）。
  const minutes = windowMinutes(range.from, range.to)
  const previousMinutes = windowMinutes(range.previousFrom, range.previousTo)
  const rpm = perMinute(totals.requests, minutes)
  const tpm = perMinute(totals.tokens, minutes)
  const previousRpm = perMinute(previousTotals.requests, previousMinutes)
  const previousTpm = perMinute(previousTotals.tokens, previousMinutes)

  if (query.isLoading) {
    return (
      <div data-slot='metric-strip' className={GRID_CLASS}>
        {CELL_IDS.map((id) => (
          <Skeleton key={id} className='rounded-control h-[100px]' />
        ))}
      </div>
    )
  }

  if (query.isError) {
    return (
      <div data-slot='metric-strip' className={GRID_CLASS}>
        <ErrorState
          className='col-span-full min-h-[100px]'
          title={t('Failed to load')}
          onRetry={() => {
            void query.refetch()
            void previousQuery.refetch()
          }}
        />
      </div>
    )
  }

  return (
    <div data-slot='metric-strip' className={GRID_CLASS}>
      <MetricTile
        icon={CELL_ICONS[0]}
        label={t('Converted cost')}
        value={formatCostYuan(totals.cost)}
        caption={t('Statistical cost')}
        delta={deltaPercent(totals.cost, previousTotals.cost)}
      />
      <MetricTile
        icon={CELL_ICONS[1]}
        label={t('Call count')}
        value={formatCount(totals.requests)}
        caption={t('Statistical count')}
        delta={deltaPercent(totals.requests, previousTotals.requests)}
      />
      <MetricTile
        icon={CELL_ICONS[2]}
        label={t('Total tokens')}
        value={formatTokenAmount(totals.tokens, i18n.language)}
        caption={t('Statistical tokens')}
        delta={deltaPercent(totals.tokens, previousTotals.tokens)}
      />
      <MetricTile
        icon={CELL_ICONS[3]}
        label={t('RPM')}
        value={formatRequestsPerMinute(rpm, i18n.language)}
        caption={t('Requests per minute')}
        delta={deltaPercent(rpm, previousRpm)}
      />
      <MetricTile
        icon={CELL_ICONS[4]}
        label={t('TPM')}
        value={formatTokensPerMinute(tpm, i18n.language)}
        caption={t('Tokens per minute')}
        delta={deltaPercent(tpm, previousTpm)}
      />
    </div>
  )
}
