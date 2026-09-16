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
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import type { ChannelModelPrice } from '../types'

// 渠道级上游单价编辑器（人民币/百万 token）。
//
// 用于成本折算：同一模型在不同上游的采购价不同，故渠道价优先于全局默认单价表。
// 这里只负责编辑，写入 setting JSON 由 channel-form.buildSettingJSON 完成。

type ChannelPricesEditorProps = {
  value: ChannelModelPrice[]
  onChange: (next: ChannelModelPrice[]) => void
  disabled?: boolean
}

function toNumber(value: string): number | undefined {
  const trimmed = value.trim()
  if (trimmed === '') return undefined
  const parsed = Number(trimmed)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined
}

export function ChannelPricesEditor(props: ChannelPricesEditorProps) {
  const { t } = useTranslation()

  const update = (index: number, key: keyof ChannelModelPrice, raw: string) => {
    const next = props.value.map((item, i) => {
      if (i !== index) return item
      if (key === 'model') return { ...item, model: raw }
      return { ...item, [key]: toNumber(raw) }
    })
    props.onChange(next)
  }

  return (
    <div className='space-y-3'>
      <div className='overflow-x-auto'>
        <table className='w-full min-w-[560px] text-sm'>
          <thead>
            <tr className='text-muted-foreground border-b text-left'>
              <th className='py-2 pr-2 font-medium'>{t('Model')}</th>
              <th className='py-2 pr-2 font-medium'>{t('Input')}</th>
              <th className='py-2 pr-2 font-medium'>{t('Output')}</th>
              <th className='py-2 pr-2 font-medium'>{t('Cache read')}</th>
              <th className='py-2 pr-2 font-medium'>{t('Cache write')}</th>
              <th className='w-10' />
            </tr>
          </thead>
          <tbody>
            {props.value.map((item, index) => (
              // eslint-disable-next-line react/no-array-index-key -- rows have no id and cannot be reordered
              <tr key={index} className='border-b last:border-0'>
                <td className='py-1.5 pr-2'>
                  <Input
                    aria-label={t('Model')}
                    value={item.model}
                    disabled={props.disabled}
                    onChange={(event) =>
                      update(index, 'model', event.target.value)
                    }
                  />
                </td>
                {(
                  ['input', 'output', 'cache_read', 'cache_write'] as const
                ).map((field) => (
                  <td key={field} className='py-1.5 pr-2'>
                    <Input
                      aria-label={t(field)}
                      inputMode='decimal'
                      value={item[field] ?? ''}
                      disabled={props.disabled}
                      onChange={(event) =>
                        update(index, field, event.target.value)
                      }
                    />
                  </td>
                ))}
                <td className='py-1.5'>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    aria-label={t('Remove')}
                    disabled={props.disabled}
                    onClick={() =>
                      props.onChange(props.value.filter((_, i) => i !== index))
                    }
                  >
                    <Trash2 className='size-4' />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Button
        type='button'
        variant='outline'
        size='sm'
        disabled={props.disabled}
        onClick={() => props.onChange([...props.value, { model: '' }])}
      >
        <Plus className='size-4' />
        {t('Add model')}
      </Button>
    </div>
  )
}
