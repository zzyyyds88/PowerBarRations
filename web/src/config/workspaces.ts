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
import { CONSOLE_ROUTES } from './routes'

export type WorkspaceId = 'connections' | 'logs'

export type WorkspaceTab = {
  id: string
  path: string
  labelKey: string
  descriptionKey: string
  /** Sidebar module gate: [section, module] in SidebarModulesAdmin. */
  module: readonly [string, string]
}

export type WorkspaceDefinition = {
  id: WorkspaceId
  base: string
  labelKey: string
  tabs: readonly WorkspaceTab[]
}

/**
 * Workspace definitions for the 5+1 console.
 *
 * A workspace is a single primary-navigation destination with real
 * sub-destinations. Tabs are ordinary routes, so refresh/back/deep links work;
 * visibility follows the existing sidebar module flags, not a new permission
 * model.
 */
export const WORKSPACES: Record<WorkspaceId, WorkspaceDefinition> = {
  connections: {
    id: 'connections',
    base: CONSOLE_ROUTES.connections,
    labelKey: 'Connections',
    tabs: [
      {
        id: 'channels',
        path: CONSOLE_ROUTES.connectionsChannels,
        labelKey: 'Channels',
        descriptionKey: 'Upstream channels, credentials, models and pricing.',
        module: ['admin', 'channel'],
      },
      {
        id: 'health',
        path: CONSOLE_ROUTES.connectionsHealth,
        labelKey: 'Channel status',
        descriptionKey:
          'Availability, success rate and latency for every upstream channel.',
        module: ['admin', 'channel'],
      },
      {
        id: 'catalog',
        path: CONSOLE_ROUTES.connectionsCatalog,
        labelKey: 'Model catalog',
        descriptionKey:
          'Model metadata, match rules and the channels that declare them.',
        module: ['admin', 'models'],
      },
    ],
  },
  logs: {
    id: 'logs',
    base: CONSOLE_ROUTES.logs,
    labelKey: 'Logs',
    tabs: [
      {
        id: 'requests',
        path: CONSOLE_ROUTES.logsRequests,
        labelKey: 'Request logs',
        descriptionKey: 'Every model-face request, including attempt chains.',
        module: ['console', 'log'],
      },
      {
        id: 'sessions',
        path: CONSOLE_ROUTES.logsSessions,
        labelKey: 'Sessions',
        descriptionKey:
          'Gateway conversations grouped by session, with context fill.',
        module: ['console', 'log'],
      },
      {
        id: 'context',
        path: CONSOLE_ROUTES.logsContext,
        labelKey: 'Context',
        descriptionKey:
          'Prompt composition and window fill for recent requests.',
        module: ['console', 'log'],
      },
      {
        id: 'masking',
        path: CONSOLE_ROUTES.logsMasking,
        labelKey: 'Masking',
        descriptionKey:
          'Privacy masking settings and the metadata-only record of what it masked.',
        module: ['console', 'log'],
      },
      {
        id: 'audit',
        path: CONSOLE_ROUTES.logsAudit,
        labelKey: 'Audit logs',
        descriptionKey: 'Administrative and configuration events.',
        module: ['console', 'audit'],
      },
      {
        id: 'tasks',
        path: CONSOLE_ROUTES.logsTasks,
        labelKey: 'System tasks',
        descriptionKey: 'Active and historical background jobs.',
        module: ['console', 'task'],
      },
    ],
  },
}

export function getVisibleWorkspaceTabs(
  workspace: WorkspaceId,
  isEnabled: (section: string, module: string) => boolean
): WorkspaceTab[] {
  return WORKSPACES[workspace].tabs.filter((tab) =>
    isEnabled(tab.module[0], tab.module[1])
  )
}

export function getDefaultWorkspacePath(
  workspace: WorkspaceId,
  isEnabled: (section: string, module: string) => boolean
): string | null {
  return getVisibleWorkspaceTabs(workspace, isEnabled)[0]?.path ?? null
}
