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
import { Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { DataTableEmptyActionProvider } from '@/components/data-table'
import { ExportCsvButton } from '@/components/export-button'
import { ConsolePage } from '@/components/layout'
import { Button } from '@/components/ui/button'

import { ApiKeysDialogs } from './components/api-keys-dialogs'
import { ApiKeysPrimaryButtons } from './components/api-keys-primary-buttons'
import { ApiKeysProvider, useApiKeys } from './components/api-keys-provider'
import { ApiKeysTable } from './components/api-keys-table'

/** 空态 CTA 复用页头的创建动作；对话框状态仍由 ApiKeysProvider 拥有。 */
function ApiKeysTableWithEmptyAction() {
  const { t } = useTranslation()
  const { setOpen } = useApiKeys()

  return (
    <DataTableEmptyActionProvider
      action={
        <Button size='sm' onClick={() => setOpen('create')}>
          <Plus className='size-4' aria-hidden='true' />
          {t('Create API Key')}
        </Button>
      }
    >
      <ApiKeysTable />
    </DataTableEmptyActionProvider>
  )
}

export function ApiKeys() {
  const { t } = useTranslation()
  return (
    <ApiKeysProvider>
      <ConsolePage
        title={t('API Keys')}
        description={t('Consumer credentials, lane access and usage.')}
        actions={
          <div className='flex flex-wrap items-center justify-end gap-2'>
            <ExportCsvButton
              request={{
                path: '/api/keys/export',
                params: { format: 'csv' },
                fallbackFilename: 'pbr-keys.csv',
              }}
            />
            <ApiKeysPrimaryButtons />
          </div>
        }
        fixedContent
      >
        <ApiKeysTableWithEmptyAction />
      </ConsolePage>

      <ApiKeysDialogs />
    </ApiKeysProvider>
  )
}
