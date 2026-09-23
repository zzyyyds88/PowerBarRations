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
// 没有全局默认单价层。计价键是**请求模型名**（路由键/车道名），不是上游真名。
// **表格初始为空，行全部按需手动添加**：可搜索下拉列出渠道模型清单中尚未
// 添加的模型，也允许键入清单外的自定义名（独立车道名等边缘场景）；每一行
// 都可删除。**未添加的模型不折算成本（免费）**，行内留空的维度按 0 计。
// 保存时仅落库至少填了一项的行；写入 setting JSON 由
// channel-form.buildSettingJSON 完成。
//
// 价格输入用草稿态保留原始键入：受控输入若在键入过程中立即归一化，键入 `0.`
// 会被 `Number("0.")` 归一成 0 并写回输入框，小数点永远打不出来（实测 bug）。
// 因此 onChange 只把归一化结果同步给上层，输入框显示原始草稿；失焦时才
// 吸附到上层已归一化的值。

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
  // 价格单元格的原始键入草稿（键 = model:field），失焦即吸附归一化值。
  const [priceDrafts, setPriceDrafts] = useState<Record<string, string>>({})

  // 展示行 = 手动添加的行 ∪ 已有计价条目（编辑回显时后端带来的价格也要可见，
  // 否则会变成表格外的隐形价格行）。
  const rows = useMemo(() => {
    const out = [...customModels]
    for (const item of props.value) {
      if (!out.includes(item.model)) out.push(item.model)
    }
    return out
  }, [customModels, props.value])

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

  const priceDraftKey = (model: string, field: PriceField) =>
    `${model}:${field}`

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

  const onPriceChange = (model: string, key: PriceField, raw: string) => {
    const draftKey = priceDraftKey(model, key)
    setPriceDrafts((prev) => ({ ...prev, [draftKey]: raw }))
    update(model, key, raw)
  }

  const onPriceBlur = (model: string, key: PriceField) => {
    const draftKey = priceDraftKey(model, key)
    setPriceDrafts((prev) => {
      if (!(draftKey in prev)) return prev
      const next = { ...prev }
      delete next[draftKey]
      return next
    })
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

  const removeModel = (model: string) => {
    setCustomModels(customModels.filter((item) => item !== model))
    setPriceDrafts((prev) => {
      const next: Record<string, string> = {}
      for (const [key, value] of Object.entries(prev)) {
        if (!key.startsWith(`${model}:`)) next[key] = value
      }
      return next
    })
    props.onChange(props.value.filter((item) => item.model !== model))
  }

  return (
    <div className='space-y-3'>
      {rows.length > 0 && (
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
                return (
                  <tr key={model} className='border-b last:border-0'>
                    <td className='py-1.5 pr-2 break-all'>{model}</td>
                    {PRICE_FIELDS.map((field) => (
                      <td key={field} className='py-1.5 pr-2'>
                        <Input
                          aria-label={`${model} ${field}`}
                          inputMode='decimal'
                          value={
                            priceDrafts[priceDraftKey(model, field)] ??
                            price?.[field] ??
                            ''
                          }
                          disabled={props.disabled}
                          onChange={(event) =>
                            onPriceChange(model, field, event.target.value)
                          }
                          onBlur={() => onPriceBlur(model, field)}
                        />
                      </td>
                    ))}
                    <td className='py-1.5'>
                      <Button
                        type='button'
                        variant='ghost'
                        size='icon-sm'
                        aria-label={t('Remove {{model}}', { model })}
                        disabled={props.disabled}
                        onClick={() => removeModel(model)}
                      >
                        <X className='size-4' aria-hidden='true' />
                      </Button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      {rows.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'No models are priced yet. Unpriced models are free (cost 0). Add rows below.'
          )}
        </p>
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
      <p className='text-muted-foreground text-xs'>
        {t(
          'Unit: CNY per 1M tokens, for cost accounting only. Blank fields count as 0; models without a row are free.'
        )}
      </p>
    </div>
  )
}
