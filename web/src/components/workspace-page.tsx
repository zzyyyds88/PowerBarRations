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
import { ConsolePage, type ConsolePageProps } from '@/components/layout'

type WorkspacePageProps = ConsolePageProps

/**
 * Page shell for a workspace destination.
 *
 * The sidebar owns workspace navigation (collapsible groups); pages do not
 * render a second tab strip. This wrapper keeps the shell import in one place
 * for the log/usage pages.
 */
export function WorkspacePage(props: WorkspacePageProps) {
  return <ConsolePage {...props} />
}
