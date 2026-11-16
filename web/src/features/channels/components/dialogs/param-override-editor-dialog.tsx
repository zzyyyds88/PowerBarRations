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

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'

import type { ParamOverrideEditorDialogProps } from './param-override-editor-constants'
import { useParamOverrideEditor } from './param-override-editor-hook'
import { ParamOverrideEditorView } from './param-override-editor-view'

export type { ParamOverrideEditorDialogProps } from './param-override-editor-constants'

export function ParamOverrideEditorDialog(
  props: ParamOverrideEditorDialogProps
) {
  const { t } = useTranslation()
  const editor = useParamOverrideEditor(props)

  return (
    <Dialog
      size='xl'
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Parameter Override')}
      description={t(
        'Create request parameter override rules with a visual editor or raw JSON.'
      )}
      contentClassName='flex max-h-[90vh] flex-col gap-0 p-0 sm:max-w-5xl'
      headerClassName='border-b px-6 py-4'
      footerClassName='border-t px-6 py-4'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' onClick={editor.handleSave}>
            {t('Save')}
          </Button>
        </>
      }
    >
      <ParamOverrideEditorView editor={editor} />
    </Dialog>
  )
}
