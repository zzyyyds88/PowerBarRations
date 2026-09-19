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
  Box,
  ClipboardList,
  FileText,
  FlaskConical,
  Key,
  LayoutDashboard,
  ListChecks,
  MessageSquare,
  Radio,
  Settings,
  Waypoints,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { SidebarData } from '@/components/layout/types'

/**
 * Root navigation groups for the application sidebar.
 *
 * These are shown when the URL does not match any nested sidebar view
 * registered in `layout/lib/sidebar-view-registry.ts`.
 *
 * PBR 保留范围（ui-spec §5）：模型/渠道/令牌/日志/仪表盘/试打/系统设置/
 * 系统任务/性能指标。多用户与计费（钱包/充值/订阅/兑换码/排名/个人中心/账号安全）
 * 已随 W7 删除，此处不得再出现入口；任务插件/异步任务入口已随 T2 删除。
 */
export function useSidebarData(): SidebarData {
  const { t } = useTranslation()

  return {
    navGroups: [
      {
        id: 'chat',
        title: t('Chat'),
        items: [
          {
            title: t('Playground'),
            url: '/playground',
            icon: FlaskConical,
          },
          {
            title: t('Chat'),
            icon: MessageSquare,
            type: 'chat-presets',
          },
        ],
      },
      {
        id: 'general',
        title: t('General'),
        items: [
          {
            // 单一「数据看板」入口，落地概览；模型调用分析与成本统计为页内 Tab
            // （ui-spec §6.2）。activeUrls 前缀匹配，切到任一 Tab 都保持高亮。
            title: t('Dashboard'),
            url: '/dashboard/overview',
            activeUrls: ['/dashboard'],
            icon: LayoutDashboard,
          },
          {
            title: t('API Keys'),
            url: '/keys',
            icon: Key,
          },
          {
            title: t('Usage Logs'),
            url: '/usage-logs/common',
            icon: FileText,
          },
          {
            title: t('Audit Logs'),
            url: '/usage-logs/audit',
            icon: ClipboardList,
          },
        ],
      },
      {
        id: 'admin',
        title: t('Admin'),
        items: [
          {
            title: t('Channel management'),
            url: '/channels',
            icon: Radio,
          },
          {
            title: t('Routing & Failover'),
            url: '/routes',
            icon: Waypoints,
          },
          {
            title: t('System Tasks'),
            url: '/system-tasks',
            icon: ListChecks,
          },
          {
            title: t('Model management'),
            url: '/models/metadata',
            icon: Box,
          },
          {
            title: t('System Settings'),
            url: '/system-settings/site',
            activeUrls: ['/system-settings'],
            icon: Settings,
          },
        ],
      },
    ],
  }
}
