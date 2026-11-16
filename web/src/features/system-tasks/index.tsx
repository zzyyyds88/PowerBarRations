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
import { useTranslation } from 'react-i18next'

import { WorkspacePage } from '@/components/workspace-page'

import { SystemTasksPanel } from './components/system-tasks-panel'

/**
 * 后台任务页：并入「日志」工作区（请求日志 / 配置审计 / 后台任务）。
 * PBR 单用户，管理面即全量权限，因此不设 Root 徽标与角色门。
 */
export function SystemTasks() {
  const { t } = useTranslation()

  return (
    <WorkspacePage
      title={t('System tasks')}
      description={t('Active and historical background jobs.')}
    >
      <SystemTasksPanel />
    </WorkspacePage>
  )
}
