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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { ConsolePage } from '@/components/layout'
import { ChannelSpeedPanel } from '@/features/channel-speed'
import type { ChannelSpeedWindow } from '@/features/channel-speed/api'
import { TimeRangeSelector } from '@/features/dashboard/components/overview/time-range-selector'
import {
  resolveTimeRange,
  type PBRTimeRangeSelection,
} from '@/features/dashboard/lib/time-range'

import { LiveRoutingCard } from './components/live-routing-card'

/**
 * 实时路由独立页（ui-spec §6 实时路由）。
 *
 * 概览只保留健康/成本/用量；拓扑与故障转移观察放到本页。时间区间与 `lane`
 * 由路由写入 URL，刷新/深链/后退都可恢复。卡片继续复用渠道状态 owner 的实现，
 * 页面只负责壳、时间区间与深链。
 */
export function LiveRoutingPage(props: {
  selection: PBRTimeRangeSelection
  onSelectionChange: (selection: PBRTimeRangeSelection) => void
  lane?: string
  onLaneChange?: (lane: string) => void
  speedWindow: ChannelSpeedWindow
  onSpeedWindowChange: (window: ChannelSpeedWindow) => void
}) {
  const { t } = useTranslation()
  const range = useMemo(
    () => resolveTimeRange(props.selection),
    [props.selection]
  )

  return (
    <ConsolePage
      title={t('Live routing')}
      description={t(
        'Lane topology, member state and per-channel output speed in real time.'
      )}
    >
      <div className='space-y-(--density-grid)'>
        <TimeRangeSelector
          selection={props.selection}
          onChange={props.onSelectionChange}
        />
        <LiveRoutingCard
          hideTitle
          range={{
            from: range.from,
            to: range.to,
            key: `${range.preset}:${range.from}:${range.to}`,
            granularity: range.granularity,
          }}
          lane={props.lane}
          onLaneChange={props.onLaneChange}
        />
        <ChannelSpeedPanel
          window={props.speedWindow}
          onWindowChange={props.onSpeedWindowChange}
        />
      </div>
    </ConsolePage>
  )
}
