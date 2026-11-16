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
import { createFileRoute } from '@tanstack/react-router'

import {
  CHANNEL_SPEED_WINDOWS,
  DEFAULT_CHANNEL_SPEED_WINDOW,
  type ChannelSpeedWindow,
} from '@/features/channel-speed/api'
import { LiveRoutingPage } from '@/features/channel-status/live-routing-page'
import type { PBRTimeRangeSelection } from '@/features/dashboard/lib/time-range'
import { liveRoutingSearchSchema } from '@/lib/search-schemas'

export const Route = createFileRoute('/_authenticated/live-routing')({
  validateSearch: liveRoutingSearchSchema,
  component: LiveRoutingRouteComponent,
})

function LiveRoutingRouteComponent() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const selection: PBRTimeRangeSelection = {
    preset: search.range ?? '24h',
    ...(search.range === 'custom' && search.from != null && search.to != null
      ? { start: new Date(search.from), end: new Date(search.to) }
      : {}),
  }
  const speedWindow: ChannelSpeedWindow = CHANNEL_SPEED_WINDOWS.includes(
    search.window as ChannelSpeedWindow
  )
    ? (search.window as ChannelSpeedWindow)
    : DEFAULT_CHANNEL_SPEED_WINDOW
  return (
    <LiveRoutingPage
      selection={selection}
      onSelectionChange={(next) => {
        void navigate({
          to: '/live-routing',
          search: {
            range: next.preset,
            from: next.preset === 'custom' ? next.start?.getTime() : undefined,
            to: next.preset === 'custom' ? next.end?.getTime() : undefined,
            lane: search.lane,
            window: speedWindow,
          },
          replace: true,
        })
      }}
      lane={search.lane}
      onLaneChange={(lane) => {
        void navigate({
          to: '/live-routing',
          search: { ...search, lane },
          replace: true,
        })
      }}
      speedWindow={speedWindow}
      onSpeedWindowChange={(window) => {
        void navigate({
          to: '/live-routing',
          search: { ...search, window },
          replace: true,
        })
      }}
    />
  )
}
