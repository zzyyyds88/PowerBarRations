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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { getLobeIcon, getLobeIconNames } from '@/lib/lobe-icon'

export function LobeIconField(props: {
  id?: string
  value: string
  onChange: (value: string) => void
  /**
   * 按模型名推断出的图标（ui-spec §6.3）。仅在未显式填写时用于展示与一键采用，
   * 不会自动写入表单。
   */
  suggestedIcon?: string
}) {
  const { t } = useTranslation()
  const options = useMemo(
    () =>
      getLobeIconNames().map((name) => ({
        value: name,
        label: name,
        icon: getLobeIcon(name, 18),
      })),
    []
  )
  const suggestedIcon = props.suggestedIcon?.trim() ?? ''
  const showSuggestion = Boolean(suggestedIcon) && !props.value
  const effectiveIcon = props.value || suggestedIcon
  return (
    <div className='space-y-3'>
      <div className='flex min-w-0 items-center gap-2'>
        <Combobox
          id={props.id}
          options={options}
          value={props.value}
          onValueChange={(value) => props.onChange(value ?? '')}
          allowCustomValue
          searchPlaceholder={t('Search icons or enter an icon key')}
          emptyText={t('No matching icons')}
          className='min-w-0 flex-1'
        />
        {props.value && (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() => props.onChange('')}
          >
            {t('Clear')}
          </Button>
        )}
      </div>
      {showSuggestion && (
        <div className='border-primary/30 bg-primary/5 flex items-center gap-3 rounded-lg border px-3 py-2'>
          <span className='flex size-8 shrink-0 items-center justify-center'>
            {getLobeIcon(suggestedIcon, 24)}
          </span>
          <div className='min-w-0 flex-1 text-xs'>
            <p className='font-medium'>
              {t('Detected from the model name')}
            </p>
            <p className='text-muted-foreground break-all'>{suggestedIcon}</p>
          </div>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => props.onChange(suggestedIcon)}
          >
            {t('Use this icon')}
          </Button>
        </div>
      )}
      <div className='bg-muted/40 flex items-center gap-3 rounded-lg border p-3'>
        <span className='flex size-9 shrink-0 items-center justify-center'>
          {getLobeIcon(effectiveIcon, 28)}
        </span>
        <div className='min-w-0 text-xs'>
          <p className='font-medium'>{t('Effective icon')}</p>
          <p className='text-muted-foreground break-all'>
            {effectiveIcon || t('Default placeholder')}
          </p>
        </div>
      </div>
      <p className='text-muted-foreground text-xs'>
        {t('Select a suggested icon or keep an existing advanced icon key.')}
      </p>
    </div>
  )
}
