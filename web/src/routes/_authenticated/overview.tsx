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
import { createFileRoute, redirect } from '@tanstack/react-router'

import { Dashboard } from '@/features/dashboard'
import type { PBRTimeRangeSelection } from '@/features/dashboard/lib/time-range'
import { overviewSearchSchema } from '@/lib/search-schemas'

export const Route = createFileRoute('/_authenticated/overview')({
  validateSearch: overviewSearchSchema,
  // 实时路由已从概览迁到独立页；旧深链 `/overview?lane=` 原地转到新地址，
  // 不把 lane 留在概览里当无效参数。
  beforeLoad: ({ search }) => {
    if (search.lane) {
      throw redirect({
        to: '/live-routing',
        search: {
          range: search.range,
          from: search.from,
          to: search.to,
          lane: search.lane,
        },
        replace: true,
      })
    }
  },
  component: OverviewRouteComponent,
})

function OverviewRouteComponent() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const selection: PBRTimeRangeSelection = {
    preset: search.range ?? '24h',
    ...(search.range === 'custom' && search.from != null && search.to != null
      ? { start: new Date(search.from), end: new Date(search.to) }
      : {}),
  }
  return (
    <Dashboard
      selection={selection}
      onSelectionChange={(next) => {
        void navigate({
          to: '/overview',
          search: {
            range: next.preset,
            from: next.preset === 'custom' ? next.start?.getTime() : undefined,
            to: next.preset === 'custom' ? next.end?.getTime() : undefined,
          },
          replace: true,
        })
      }}
    />
  )
}
