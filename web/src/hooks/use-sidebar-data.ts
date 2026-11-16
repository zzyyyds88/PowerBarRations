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
import {
  Activity,
  BarChart3,
  Boxes,
  FileText,
  Key,
  LayoutDashboard,
  ListChecks,
  MessageSquare,
  Layers,
  Radio,
  Settings,
  ShieldCheck,
  Waypoints,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { SidebarData } from '@/components/layout/types'
import { CONSOLE_ROUTES } from '@/config/routes'

/**
 * Root navigation groups for the application sidebar.
 *
 * Grouped IA: the operator works by question, not by backend table. Usage
 * (overview/requests/sessions/context) and Channels & models are collapsible
 * groups; Masking is its own entry, and audit/tasks live with Settings in the
 * System section. Pages no longer carry a second tab strip. Playground is a
 * global header/command action, not a nav item.
 */
export function useSidebarData(): SidebarData {
  const { t } = useTranslation()

  return {
    navGroups: [
      {
        id: 'general',
        title: t('Nav console'),
        items: [
          {
            title: t('Nav live routing'),
            url: CONSOLE_ROUTES.liveRouting,
            icon: Waypoints,
          },
          {
            title: t('Nav routing'),
            url: CONSOLE_ROUTES.routing,
            icon: Waypoints,
          },
          {
            title: t('Nav channels and models'),
            icon: Radio,
            activeUrls: [
              CONSOLE_ROUTES.connectionsChannels,
              CONSOLE_ROUTES.connectionsHealth,
              CONSOLE_ROUTES.connectionsCatalog,
            ],
            items: [
              {
                title: t('Nav channels'),
                url: CONSOLE_ROUTES.connectionsChannels,
                configUrls: [CONSOLE_ROUTES.connectionsChannels],
                icon: Radio,
              },
              {
                title: t('Nav channel status'),
                url: CONSOLE_ROUTES.connectionsHealth,
                configUrls: [CONSOLE_ROUTES.connectionsHealth],
                icon: Activity,
              },
              {
                title: t('Nav model catalog'),
                url: CONSOLE_ROUTES.connectionsCatalog,
                configUrls: [CONSOLE_ROUTES.connectionsCatalog],
                icon: Boxes,
              },
            ],
          },
          {
            title: t('Nav api keys'),
            url: CONSOLE_ROUTES.keys,
            icon: Key,
          },
          {
            title: t('Nav usage'),
            icon: BarChart3,
            activeUrls: [
              CONSOLE_ROUTES.overview,
              CONSOLE_ROUTES.logsRequests,
              CONSOLE_ROUTES.logsSessions,
              CONSOLE_ROUTES.logsContext,
            ],
            items: [
              {
                title: t('Nav overview'),
                url: CONSOLE_ROUTES.overview,
                icon: LayoutDashboard,
              },
              {
                title: t('Nav requests'),
                url: CONSOLE_ROUTES.logsRequests,
                icon: FileText,
              },
              {
                title: t('Nav sessions'),
                url: CONSOLE_ROUTES.logsSessions,
                icon: MessageSquare,
              },
              {
                title: t('Nav context'),
                url: CONSOLE_ROUTES.logsContext,
                icon: Layers,
              },
            ],
          },
          {
            title: t('Nav masking'),
            url: CONSOLE_ROUTES.logsMasking,
            activeUrls: [CONSOLE_ROUTES.logsMasking],
            icon: ShieldCheck,
          },
        ],
      },
      {
        id: 'admin',
        title: t('Nav system'),
        items: [
          {
            title: t('Nav audit logs'),
            url: CONSOLE_ROUTES.logsAudit,
            icon: FileText,
          },
          {
            title: t('Nav system tasks'),
            url: CONSOLE_ROUTES.logsTasks,
            icon: ListChecks,
          },
          {
            title: t('Nav settings'),
            icon: Settings,
            activeUrls: ['/system-settings'],
            items: [
              {
                title: t('Site & Branding'),
                url: '/system-settings/site',
                activeUrls: ['/system-settings/site'],
                icon: LayoutDashboard,
              },
              {
                title: t('Models & Routing'),
                url: '/system-settings/models',
                activeUrls: ['/system-settings/models'],
                icon: Boxes,
              },
              {
                title: t('Console Content'),
                url: '/system-settings/content',
                activeUrls: ['/system-settings/content'],
                icon: FileText,
              },
              {
                title: t('Operations'),
                url: '/system-settings/operations',
                activeUrls: ['/system-settings/operations'],
                icon: Settings,
              },
            ],
          },
        ],
      },
    ],
  }
}
