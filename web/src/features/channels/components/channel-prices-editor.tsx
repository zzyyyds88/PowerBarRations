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
import { X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import type { ComboboxInputOption } from '@/components/ui/combobox-input'
import { Input } from '@/components/ui/input'

import type { ChannelModelPrice } from '../types'

// 渠道级上游单价编辑器（人民币/百万 token，单层单价：design-v1 §16.9#7）。
//
// 只用于成本折算：同一模型在不同上游的采购价不同，价格唯一来源就是渠道价，
// 没有全局默认单价层。计价键是**请求模型名**（路由键/车道名），不是上游真名
// ——车道成员可引用清单之外的上游名，因此除渠道模型清单外还支持添加清单外
// 的自定义计价行（可删除）。添加行用可搜索下拉（allowCustomValue）：选项 =
// 渠道模型清单中尚未出现在表格里的模型，也允许键入清单外的自定义名。
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

  // 已配价但不在模型清单里的模型（如独立车道名）也要可见、可删除，否则会变成
  // 表格外的隐形价格行。它们与手动添加的自定义行一样按清单外行处理。
  const pricedCustomModels = useMemo(
    () =>
      props.value
        .map((item) => item.model)
        .filter(
          (model) =>
            model !== '' &&
            !props.models.includes(model) &&
            !customModels.includes(model)
        ),
    [props.value, props.models, customModels]
  )

  const rows = useMemo(
    () => [
      ...new Set([...props.models, ...customModels, ...pricedCustomModels]),
    ],
    [props.models, customModels, pricedCustomModels]
  )
  const isCustom = (model: string) => !props.models.includes(model)

  // 可搜索下拉的选项：渠道模型清单中尚未出现在表格里的模型；allowCustomValue
  // 允许键入清单外的自定义名（独立车道名等边缘场景）。
  const addOptions = useMemo<ComboboxInputOption[]>(
    () =>
      props.models
        .filter((model) => !rows.includes(model))
        .map((model) => ({ value: model, label: model })),
    [props.models, rows]
  )
  const addOptionValues = useMemo(
    () => new Set(addOptions.map((option) => option.value)),
    [addOptions]
  )

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

  const addCustomModel = (rawModel: string) => {
    const model = rawModel.trim()
    if (!model || rows.includes(model)) {
      setCustomDraft('')
      return
    }
    setCustomModels([...customModels, model])
    setCustomDraft('')
  }

  // Combobox 的 onValueChange 在键入过程中逐字符触发（allowCustomValue 模式），
  // 也会在"选中选项"与"对键入的自定义名按回车"时各触发一次：
  // - 命中选项 → 直接添加该行；
  // - 与当前草稿一致（即回车确认，而不是逐字符输入）→ 添加该行；
  // - 其余情况只更新草稿。
  const handleDraftChange = (value: string | null) => {
    const model = (value ?? '').trim()
    if (!model) {
      setCustomDraft('')
      return
    }
    if (addOptionValues.has(model) || model === customDraft.trim()) {
      addCustomModel(model)
      return
    }
    setCustomDraft(model)
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

      {/* 敏感信息锁定时隐藏添加入口（组件不支持 disabled，锁定即不可加行）。 */}
      {!props.disabled && (
        <Combobox
          options={addOptions}
          allowCustomValue
          value={customDraft}
          placeholder={t('Select from channel models or type a model name')}
          aria-label={t('Add a model to price')}
          onValueChange={handleDraftChange}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              addCustomModel(customDraft)
            }
          }}
        />
      )}
    </div>
  )
}
