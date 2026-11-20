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
import { useTranslation } from 'react-i18next'

import { ConsolePage } from '@/components/layout'
import { ChannelSpeedPanel } from '@/features/channel-speed'

import {
  LiveRoutingCard,
  type LiveRoutingStatsRange,
} from './components/live-routing-card'

/**
 * 车道窗口统计的固定窗口：最近 24 小时（小时桶）。
 *
 * 本页是观察面，不是分析面——操作员进来是看「现在流量在走哪条腿」，不该先被要求
 * 选一个时间区间。历史区间分析在概览页（数据概览）里做。
 */
const LIVE_STATS_WINDOW_HOURS = 24
const HOUR_SECONDS = 60 * 60

/**
 * 当前 24h 窗口。`to` 对齐到整点，所以窗口与查询键在同一个小时内保持不变，
 * 不会因为每次渲染重算而反复重取；跨过整点后自动前移。
 */
function currentLiveStatsRange(): LiveRoutingStatsRange {
  const to = Math.floor(Date.now() / 1000 / HOUR_SECONDS) * HOUR_SECONDS
  const from = to - LIVE_STATS_WINDOW_HOURS * HOUR_SECONDS
  return {
    from,
    to,
    key: `live-${LIVE_STATS_WINDOW_HOURS}h:${to}`,
    granularity: 'hour',
  }
}

/**
 * 实时路由独立页（ui-spec §6 实时路由）。
 *
 * 概览只保留健康/成本/用量；拓扑与故障转移观察放到本页。页面只负责壳与 `lane`
 * 深链，卡片继续复用渠道状态 owner 的实现。
 *
 * 本页**不提供时间区间选择**：拓扑与渠道速度都是实时量，窗口固定
 * （车道统计 24h、渠道速度 30s），避免「实时」页面上挂一个会误导的历史区间。
 */
export function LiveRoutingPage(props: {
  lane?: string
  onLaneChange?: (lane: string) => void
}) {
  const { t } = useTranslation()

  return (
    <ConsolePage
      title={t('Live routing')}
      description={t(
        'Lane topology, member state and per-channel output speed in real time.'
      )}
    >
      <div className='space-y-(--density-grid)'>
        <LiveRoutingCard
          hideTitle
          range={currentLiveStatsRange()}
          lane={props.lane}
          onLaneChange={props.onLaneChange}
        />
        <ChannelSpeedPanel />
      </div>
    </ConsolePage>
  )
}
