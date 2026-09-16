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
import { useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { useAuthStore } from '@/stores/auth-store'

import { AuditLogViewer } from './components/audit-log-viewer'

/**
 * 审计页（PBR 单用户）。
 *
 * 只有"全部"一个视图：管理面只有一把凭据、一个身份，`/api/console/audit/self`
 * 既未注册也没有语义（审查 F18）；此前"Only Mine"标签会打一个 404 端点。
 */
export function AuditLogs() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const queryClient = useQueryClient()
  const userId = user?.id
  const handleAccessDenied = useCallback(async () => {
    // PBR 是单用户网关，权限恒为全量；这里只把已缓存的列表清掉强制重取。
    await queryClient.cancelQueries({ queryKey: ['audit', userId, 'all'] })
    queryClient.removeQueries({ queryKey: ['audit', userId, 'all'] })
  }, [queryClient, userId])

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Audit Logs')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-3'>
          <div className='min-h-0 flex-1'>
            <AuditLogViewer
              key={`${userId}:all`}
              onAccessDenied={handleAccessDenied}
              scope='all'
            />
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
