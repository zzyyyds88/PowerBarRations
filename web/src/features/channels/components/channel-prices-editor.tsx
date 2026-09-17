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
import { Plus, X } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import type { ChannelModelPrice } from '../types'

// 渠道级上游单价编辑器（人民币/百万 token）。
//
// 用于成本折算：同一模型在不同上游的采购价不同，故渠道价优先于全局默认单价表。
// 计价键是**请求模型名**（路由键/车道名），不是上游真名——车道成员可引用清单
// 之外的上游名，因此除渠道模型清单外还支持手动添加自定义计价行（可删除）。
// 保存时仅落库至少填了一项的模型；这里只负责编辑，写入 setting JSON 由
// channel-form.buildSettingJSON 完成。

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
  const [customDraft, setCustomDraft] = useState('')
  const [customModels, setCustomModels] = useState<string[]>([])

  const rows = [...props.models, ...customModels]
  const isCustom = (model: string) => !props.models.includes(model)

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

  const addCustomModel = () => {
    const model = customDraft.trim()
    if (!model || rows.includes(model)) {
      setCustomDraft('')
      return
    }
    setCustomModels([...customModels, model])
    setCustomDraft('')
  }

  const removeCustomModel = (model: string) => {
    setCustomModels(customModels.filter((item) => item !== model))
    props.onChange(props.value.filter((item) => item.model !== model))
  }

  return (
    <div className='space-y-3'>
      {rows.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('Add models first, then set their upstream prices.')}
        </p>
      ) : (
        <div className='overflow-x-auto'>
          <table className='w-full min-w-[560px] text-sm'>
            <thead>
              <tr className='text-muted-foreground border-b text-left'>
                <th className='py-2 pr-2 font-medium'>{t('Model')}</th>
                <th className='py-2 pr-2 font-medium'>{t('Input')}</th>
                <th className='py-2 pr-2 font-medium'>{t('Output')}</th>
                <th className='py-2 pr-2 font-medium'>{t('Cache read')}</th>
                <th className='py-2 pr-2 font-medium'>{t('Cache write')}</th>
                <th className='w-8 py-2' aria-hidden='true' />
              </tr>
            </thead>
            <tbody>
              {rows.map((model) => {
                const price = props.value.find((item) => item.model === model)
                const custom = isCustom(model)
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
                    <td className='py-1.5'>
                      {custom && (
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon-sm'
                          aria-label={t('Remove {{model}}', { model })}
                          disabled={props.disabled}
                          onClick={() => removeCustomModel(model)}
                        >
                          <X className='size-4' aria-hidden='true' />
                        </Button>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      <div className='flex gap-2'>
        <Input
          value={customDraft}
          placeholder={t('Add a model to price (e.g. a lane name)')}
          disabled={props.disabled}
          onChange={(event) => setCustomDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              addCustomModel()
            }
          }}
        />
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.disabled || !customDraft.trim()}
          onClick={addCustomModel}
        >
          <Plus data-icon='inline-start' />
          {t('Add')}
        </Button>
      </div>
    </div>
  )
}
