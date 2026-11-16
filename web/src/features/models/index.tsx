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
import { ConsolePage } from '@/components/layout'
import { Button } from '@/components/ui/button'

import { ModelsDialogs } from './components/models-dialogs'
import { ModelsPrimaryButtons } from './components/models-primary-buttons'
import { ModelsProvider, useModels } from './components/models-provider'
import { ModelsRoleHint } from './components/models-role-hint'
import { ModelsTable } from './components/models-table'

/** 目录空态复用页头的「新建模型」；对话框状态仍由 ModelsProvider 拥有。 */
function ModelsTableWithEmptyAction() {
  const { t } = useTranslation()
  const { setOpen, setCurrentRow } = useModels()

  return (
    <DataTableEmptyActionProvider
      action={
        <Button
          size='sm'
          onClick={() => {
            setCurrentRow(null)
            setOpen('create-model')
          }}
        >
          <Plus className='size-4' aria-hidden='true' />
          {t('Add Model')}
        </Button>
      }
    >
      <ModelsTable />
    </DataTableEmptyActionProvider>
  )
}

function ModelsContent() {
  const { t } = useTranslation()

  return (
    <>
      <ConsolePage
        title={t('Model catalog')}
        description={t(
          'Model metadata, match rules and the channels that declare them.'
        )}
        actions={<ModelsPrimaryButtons />}
        stackActionsOnMobile
        fixedContent
      >
        <div className='flex h-full min-h-0 flex-col gap-3'>
          <ModelsRoleHint />
          <div className='min-h-0 flex-1'>
            <ModelsTableWithEmptyAction />
          </div>
        </div>
      </ConsolePage>

      <ModelsDialogs />
    </>
  )
}

export function Models() {
  return (
    <ModelsProvider>
      <ModelsContent />
    </ModelsProvider>
  )
}
