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

import { SectionPageLayout } from '@/components/layout'

import { SystemTasksPanel } from './components/system-tasks-panel'

/**
 * 系统任务独立页（ui-spec §6.10）：原 system-info 页里的任务面板提为侧边栏
 * 独立页。PBR 单用户，管理面即全量权限，因此不设 Root 徽标与角色门。
 */
export function SystemTasks() {
  const { t } = useTranslation()

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='truncate'>{t('System Tasks')}</span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <SystemTasksPanel />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
