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
import { usePageUrlState } from '@/hooks/use-page-url-state'

import { UserInfoDialog } from './components/dialogs/user-info-dialog'
import { RequestLogsExportMenu } from './components/request-logs-export-menu'
import { RequestLogsTailSelect } from './components/request-logs-tail-select'
import {
  UsageLogsProvider,
  useUsageLogsContext,
} from './components/usage-logs-provider'
import { UsageLogsTable } from './components/usage-logs-table'

function UsageLogsContent() {
  const { t } = useTranslation()
  const { search } = usePageUrlState()
  const { selectedUserId, userInfoDialogOpen, setUserInfoDialogOpen } =
    useUsageLogsContext()

  return (
    <>
      <WorkspacePage
        title={t('Request logs')}
        description={t('Every model-face request, including attempt chains.')}
        actions={
          <div className='flex flex-wrap items-center justify-end gap-2'>
            <RequestLogsTailSelect />
            <RequestLogsExportMenu search={search} />
          </div>
        }
        fixedContent
      >
        <div className='flex h-full min-h-0 flex-col'>
          <div className='min-h-0 flex-1'>
            <UsageLogsTable logCategory='pbr' />
          </div>
        </div>
      </WorkspacePage>

      <UserInfoDialog
        userId={selectedUserId}
        open={userInfoDialogOpen}
        onOpenChange={setUserInfoDialogOpen}
      />
    </>
  )
}

export function UsageLogs() {
  return (
    <UsageLogsProvider>
      <UsageLogsContent />
    </UsageLogsProvider>
  )
}
