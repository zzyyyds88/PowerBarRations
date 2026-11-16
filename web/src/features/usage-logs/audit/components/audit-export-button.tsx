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
import { ExportCsvButton } from '@/components/export-button'

import type { AuditFilters } from '../api'

const DAY_SECONDS = 24 * 60 * 60
/** `/api/audit/export` requires a window; default to the last 7 days. */
const DEFAULT_WINDOW_SECONDS = 7 * DAY_SECONDS

function toIso(seconds: number): string {
  return new Date(seconds * 1000).toISOString()
}

/**
 * Audit CSV export (api-spec §5.5.2).
 *
 * `GET /api/audit/export` requires an RFC3339 `from`/`to` pair of at most 31
 * days — unlike the list endpoint, it has no implicit range. The button
 * therefore forwards the page's date filter when set, and falls back to the
 * last 7 days otherwise (never a silent unbounded export). The list's other
 * filters (success/category/token/username/request_id) have no counterpart in
 * the export contract and are not invented here.
 *
 * The request is built in the click handler so `Date.now()` never runs
 * during render (React purity) and the window is current at export time.
 */
export function AuditExportButton(props: {
  filters: AuditFilters
  disabled?: boolean
}) {
  return (
    <ExportCsvButton
      disabled={props.disabled}
      request={() => {
        const end = props.filters.end_timestamp ?? Math.floor(Date.now() / 1000)
        const start =
          props.filters.start_timestamp ?? end - DEFAULT_WINDOW_SECONDS
        const from = toIso(start)
        const to = toIso(end)
        return {
          path: '/api/audit/export',
          params: { format: 'csv', from, to },
          fallbackFilename: `pbr-audit-${from.slice(0, 10)}-${to.slice(0, 10)}.csv`,
        }
      }}
    />
  )
}
