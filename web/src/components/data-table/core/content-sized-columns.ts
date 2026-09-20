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
/**
 * Columns that hug their content width (colgroup `1%`) instead of taking a
 * proportional share of the table width. Opt in per column via
 * `meta.contentSized`; the row-action column (`actions`) is content-sized
 * unconditionally.
 */
interface ContentSizedCandidate {
  id: string
  columnDef?: { meta?: { contentSized?: boolean } }
}

export function isContentSizedColumn(column: ContentSizedCandidate): boolean {
  if (column.columnDef?.meta?.contentSized === true) {
    return true
  }
  return column.id === 'actions'
}
