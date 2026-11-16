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
import type { ReactNode } from 'react'

import { DataTableEmptyActionContext } from '../core/empty-action-context'

/**
 * Page-level default empty-state action (ui-spec §12).
 *
 * List pages that own a creation flow render the table inside this provider
 * while the create button itself stays in the page `actions` cluster. The
 * table then shows the same CTA in its empty state without either component
 * importing the other's dialogs. A table can still pass `emptyAction`
 * directly to `DataTablePage`; an explicit prop always wins.
 */
export function DataTableEmptyActionProvider(props: {
  action: ReactNode
  children: ReactNode
}) {
  return (
    <DataTableEmptyActionContext.Provider value={props.action}>
      {props.children}
    </DataTableEmptyActionContext.Provider>
  )
}
