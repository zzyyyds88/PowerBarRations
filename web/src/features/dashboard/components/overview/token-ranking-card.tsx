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
// 首页概览「Token 排行榜」卡（ui-spec §6.2A 第 2 块，右 1/3）。
//
// Top 10 模型（GET /api/model-status，按 tokens_total 降序）：
// 圆角图标（无真实 logo 用首字母占位）+ 模型名 + 服务它的渠道 + token 总数（B/M/K）。
//
// 「服务它的渠道」的数据缺口：/api/model-status 按**上游模型**聚合，不带渠道字段；
// 渠道取自同一窗口的 /api/stats?group_by=channel_model，按该模型的请求数取主导渠道。
// 两个维度的模型名口径不同（渠道映射后可能不相等），取不到就省略这行。

import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { getAvatarColorClass } from '@/lib/colors'
import { cn } from '@/lib/utils'

import { formatTokenAmount, modelInitial } from '../../lib/format'
import type { PBRResolvedTimeRange } from '../../lib/time-range'
import { getPBRModelStatus } from '../../model-status-api'
import {
  PBR_CHANNEL_MODEL_SEPARATOR,
  getPBRStats,
  type PBRStatBucket,
} from '../../pbr-stats-api'
import { PanelWrapper } from '../ui/panel-wrapper'

const TOP_LIMIT = 10
const STALE_TIME_MS = 60 * 1000
const EMPTY_BUCKETS: PBRStatBucket[] = []

/** 每个模型的请求数按渠道汇总，取请求数最多的渠道作为「服务它的渠道」。 */
function dominantChannelByModel(items: PBRStatBucket[]): Map<string, string> {
  const byModel = new Map<string, Map<string, number>>()
  for (const item of items) {
    const [channel, model] = item.group.split(PBR_CHANNEL_MODEL_SEPARATOR)
    if (!channel || !model) continue
    const perChannel = byModel.get(model) ?? new Map<string, number>()
    perChannel.set(
      channel,
      (perChannel.get(channel) ?? 0) + (item.requests || 0)
    )
    byModel.set(model, perChannel)
  }
  const dominant = new Map<string, string>()
  for (const [model, perChannel] of byModel) {
    let bestChannel = ''
    let bestRequests = -1
    for (const [channel, requests] of perChannel) {
      if (requests > bestRequests) {
        bestChannel = channel
        bestRequests = requests
      }
    }
    if (bestChannel) dominant.set(model, bestChannel)
  }
  return dominant
}

export function TokenRankingCard(props: { range: PBRResolvedTimeRange }) {
  const { t, i18n } = useTranslation()
  const range = props.range

  const statusQuery = useQuery({
    queryKey: ['dashboard', 'overview', 'model-status', range.from, range.to],
    queryFn: () => getPBRModelStatus({ from: range.from, to: range.to }),
    staleTime: STALE_TIME_MS,
    refetchOnWindowFocus: false,
  })

  const channelQuery = useQuery({
    queryKey: [
      'dashboard',
      'overview',
      'channel-model',
      range.from,
      range.to,
      range.granularity,
    ],
    queryFn: () =>
      getPBRStats({
        granularity: range.granularity,
        from: range.from,
        to: range.to,
        groupBy: 'channel_model',
      }),
    staleTime: STALE_TIME_MS,
    refetchOnWindowFocus: false,
  })

  const rows = useMemo(() => {
    const items = statusQuery.data?.items ?? []
    return [...items]
      .sort(
        (left, right) =>
          (right.tokens_total || 0) - (left.tokens_total || 0) ||
          left.model.localeCompare(right.model)
      )
      .slice(0, TOP_LIMIT)
  }, [statusQuery.data])

  const channels = useMemo(
    () => dominantChannelByModel(channelQuery.data?.items ?? EMPTY_BUCKETS),
    [channelQuery.data]
  )

  const isEmpty =
    !statusQuery.isLoading && !statusQuery.isError && rows.length === 0

  let body = (
    <ul className='flex flex-col gap-2.5' aria-label={t('Token ranking')}>
      {rows.map((item) => {
        const channel = channels.get(item.model)
        return (
          <li key={item.model} className='flex items-center gap-2.5'>
            <span
              aria-hidden='true'
              className={cn(
                'shadow-control flex size-8 shrink-0 items-center justify-center rounded-control text-xs font-semibold',
                getAvatarColorClass(item.model)
              )}
            >
              {modelInitial(item.model)}
            </span>
            <span className='min-w-0 flex-1'>
              <span
                className='block truncate font-mono text-xs'
                title={item.model}
              >
                {item.model}
              </span>
              {channel != null && (
                <span className='text-muted-foreground block truncate text-xs'>
                  {t('Served by {{channel}}', { channel })}
                </span>
              )}
            </span>
            <span className='shrink-0 font-mono text-xs font-semibold tabular-nums'>
              {formatTokenAmount(item.tokens_total, i18n.language)}
            </span>
          </li>
        )
      })}
    </ul>
  )
  if (statusQuery.isError) {
    body = (
      <ErrorState
        className='min-h-[200px]'
        title={t('Failed to load')}
        onRetry={() => void statusQuery.refetch()}
      />
    )
  }

  return (
    <PanelWrapper
      title={t('Token ranking')}
      loading={statusQuery.isLoading}
      empty={isEmpty}
      emptyMessage={t('No data in the selected range')}
      height='h-[320px]'
    >
      {body}
    </PanelWrapper>
  )
}
