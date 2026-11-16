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
 * Console route constants — the only place new IA paths are written.
 *
 * Old paths live in `src/lib/legacy-route.ts` and always redirect here.
 * Navigation, workspace tabs and redirect targets must import these
 * constants instead of spelling URLs inline.
 */
export const CONSOLE_ROUTES = {
  overview: '/overview',
  liveRouting: '/live-routing',
  routing: '/routes',
  connections: '/connections',
  connectionsChannels: '/connections/channels',
  connectionsHealth: '/connections/health',
  connectionsCatalog: '/connections/catalog',
  keys: '/keys',
  logs: '/logs',
  logsRequests: '/logs/requests',
  logsSessions: '/logs/sessions',
  logsContext: '/logs/context',
  logsMasking: '/logs/masking',
  logsAudit: '/logs/audit',
  logsTasks: '/logs/tasks',
  settings: '/system-settings/site',
  playground: '/playground',
} as const

export type ConsoleRoute = (typeof CONSOLE_ROUTES)[keyof typeof CONSOLE_ROUTES]

export const CONSOLE_ROUTE_PATHS = new Set<string>(
  Object.values(CONSOLE_ROUTES)
)
