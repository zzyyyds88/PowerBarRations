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

import { ChannelsDialogs } from './components/channels-dialogs'
import { ChannelsPrimaryButtons } from './components/channels-primary-buttons'
import { ChannelsProvider, useChannels } from './components/channels-provider'
import { ChannelsTable } from './components/channels-table'

/**
 * 表格空态复用页头的创建动作（ui-spec §12：有创建动作的页面必须给 CTA）。
 * 创建对话框状态仍由 ChannelsProvider 拥有，这里只接线，不复制业务逻辑。
 */
function ChannelsTableWithEmptyAction() {
  const { t } = useTranslation()
  const { setOpen, setCurrentRow } = useChannels()

  return (
    <DataTableEmptyActionProvider
      action={
        <Button
          size='sm'
          onClick={() => {
            setCurrentRow(null)
            setOpen('create-channel')
          }}
        >
          <Plus className='size-4' aria-hidden='true' />
          {t('Create Channel')}
        </Button>
      }
    >
      <ChannelsTable />
    </DataTableEmptyActionProvider>
  )
}

export function Channels() {
  const { t } = useTranslation()

  return (
    <ChannelsProvider>
      <ConsolePage
        title={t('Channels')}
        description={t(
          'Upstream channels, credentials, declared models and pricing.'
        )}
        actions={
          <div className='flex flex-wrap items-center justify-end gap-2'>
            <ExportCsvButton
              request={{
                path: '/api/channels/export',
                params: { format: 'csv' },
                fallbackFilename: 'pbr-channels.csv',
              }}
            />
            <ChannelsPrimaryButtons />
          </div>
        }
        fixedContent
      >
        <ChannelsTableWithEmptyAction />
      </ConsolePage>

      <ChannelsDialogs />
    </ChannelsProvider>
  )
}
