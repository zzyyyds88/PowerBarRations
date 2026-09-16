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

import { Input } from '@/components/ui/input'

import type { ChannelModelPrice } from '../types'

// 渠道级上游单价编辑器（人民币/百万 token）。
//
// 用于成本折算：同一模型在不同上游的采购价不同，故渠道价优先于全局默认单价表。
// 表格跟随渠道模型清单：每个模型一行、模型名只读，保存时仅落库至少填了一项的模型。
// 这里只负责编辑，写入 setting JSON 由 channel-form.buildSettingJSON 完成。

const PRICE_FIELDS = ['input', 'output', 'cache_read', 'cache_write'] as const

type PriceField = (typeof PRICE_FIELDS)[number]

type ChannelPricesEditorProps = {
  value: ChannelModelPrice[]
  models: string[]
  onChange: (next: ChannelModelPrice[]) => void
  disabled?: boolean
}

function toNumber(value: string): number | undefined {
  const trimmed = value.trim()
  if (trimmed === '') return undefined
  const parsed = Number(trimmed)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined
}

function createPrice(
  model: string,
  key: PriceField,
  value: number | undefined
): ChannelModelPrice {
  const price: ChannelModelPrice = { model }
  price[key] = value
  return price
}

export function ChannelPricesEditor(props: ChannelPricesEditorProps) {
  const { t } = useTranslation()

  const update = (model: string, key: PriceField, raw: string) => {
    const value = toNumber(raw)
    if (props.value.some((item) => item.model === model)) {
      props.onChange(
        props.value.map(
          (item): ChannelModelPrice =>
            item.model === model ? { ...item, [key]: value } : item
        )
      )
      return
    }
    props.onChange([...props.value, createPrice(model, key, value)])
  }

  if (props.models.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('Add models first, then set their upstream prices.')}
      </p>
    )
  }

  return (
    <div className='overflow-x-auto'>
      <table className='w-full min-w-[560px] text-sm'>
        <thead>
          <tr className='text-muted-foreground border-b text-left'>
            <th className='py-2 pr-2 font-medium'>{t('Model')}</th>
            <th className='py-2 pr-2 font-medium'>{t('Input')}</th>
            <th className='py-2 pr-2 font-medium'>{t('Output')}</th>
            <th className='py-2 pr-2 font-medium'>{t('Cache read')}</th>
            <th className='py-2 pr-2 font-medium'>{t('Cache write')}</th>
          </tr>
        </thead>
        <tbody>
          {props.models.map((model) => {
            const price = props.value.find((item) => item.model === model)
            return (
              <tr key={model} className='border-b last:border-0'>
                <td className='py-1.5 pr-2 break-all'>{model}</td>
                {PRICE_FIELDS.map((field) => (
                  <td key={field} className='py-1.5 pr-2'>
                    <Input
                      aria-label={`${model} ${field}`}
                      inputMode='decimal'
                      value={price?.[field] ?? ''}
                      disabled={props.disabled}
                      onChange={(event) =>
                        update(model, field, event.target.value)
                      }
                    />
                  </td>
                ))}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
