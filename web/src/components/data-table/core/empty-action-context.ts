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
import { createContext, useContext, type ReactNode } from 'react'

/**
 * Page-level default empty-state action (ui-spec §12).
 *
 * Kept in a non-component module so the provider file stays a pure
 * component module (React Fast Refresh) and table code can read the value
 * without importing the provider.
 */
export const DataTableEmptyActionContext = createContext<ReactNode>(undefined)

export function useDataTableDefaultEmptyAction(): ReactNode {
  return useContext(DataTableEmptyActionContext)
}
