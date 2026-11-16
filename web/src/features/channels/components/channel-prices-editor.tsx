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
import { ClipboardPaste, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import type { ComboboxInputOption } from '@/components/ui/combobox-input'

import {
  PRICE_FIELDS,
  effectivePriceAt,
  type PriceField,
  type PricedChannelModel,
} from '../lib/channel-prices'
import type { ChannelModelPrice } from '../types'
import { ChannelPriceBandsEditor } from './channel-price-bands-editor'
import { ChannelPriceImportPanel } from './channel-price-import-panel'
import { PriceNumberInput } from './channel-price-number-input'

// 渠道级上游单价编辑器（人民币/百万 token；design-v1 §16.9#7、ui-spec §9）。
//
// 只用于成本折算：同一模型在不同上游的采购价不同，价格唯一来源就是渠道价，
// 没有全局默认单价层。计价键是**请求模型名**（路由键/车道名），不是上游真名。
// **表格初始为空，行全部按需手动添加**：可搜索下拉列出渠道模型清单中尚未
// 添加的模型，也允许键入清单外的自定义名（独立车道名等边缘场景）；每一行
// 都可删除。**未添加的模型不折算成本（免费）**，行内留空的维度按 0 计。
// 保存时仅落库至少填了一项的行（或带峰谷时段的行）；写入 setting JSON 由
// channel-form.buildSettingJSON 调用 normalizePricesForSetting 完成。
//
// 峰谷（可选，ui-spec §9）：每行可展开「峰谷时段」面板配置 timezone 与有序
// bands；行级四个价格即全天兜底价。旧单层价（无 bands）按全天兜底显示与
// 保存，**不会**被强行补出 bands。
//
// 价格输入用草稿态保留原始键入（见 PriceNumberInput）：受控输入若在键入
// 过程中立即归一化，键入 `0.` 会被 Number("0.") 归一成 0 并写回输入框，
// 小数点永远打不出来（实测 bug）。

/** 当前时间的低频心跳：只驱动「当前生效价」预览，不参与表单状态。 */
function usePriceClock(): Date {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 30_000)
    return () => window.clearInterval(timer)
  }, [])
  return now
}

type ChannelPricesEditorProps = {
  value: ChannelModelPrice[]
  models: string[]
  onChange: (next: ChannelModelPrice[]) => void
  disabled?: boolean
}

export function ChannelPricesEditor(props: ChannelPricesEditorProps) {
  const { t } = useTranslation()
  const [customDraft, setCustomDraft] = useState('')
  const [customModels, setCustomModels] = useState<string[]>([])
  const [expandedModel, setExpandedModel] = useState<string | null>(null)
  const [importOpen, setImportOpen] = useState(false)
  const now = usePriceClock()
  const value = props.value as PricedChannelModel[]

  // 展示行 = 手动添加的行 ∪ 已有计价条目（编辑回显时后端带来的价格也要可见，
  // 否则会变成表格外的隐形价格行）。
  const rows = useMemo(() => {
    const out = [...customModels]
    for (const item of value) {
      if (!out.includes(item.model)) out.push(item.model)
    }
    return out
  }, [customModels, value])

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

  const patchRow = (model: string, patch: Partial<PricedChannelModel>) => {
    if (value.some((item) => item.model === model)) {
      props.onChange(
        value.map((item) =>
          item.model === model ? { ...item, ...patch } : item
        )
      )
      return
    }
    props.onChange([...value, { model, ...patch }])
  }

  const setPrice = (
    model: string,
    field: PriceField,
    nextValue: number | undefined
  ) => {
    if (value.some((item) => item.model === model)) {
      props.onChange(
        value.map((item) => {
          if (item.model !== model) return item
          const next: PricedChannelModel = { ...item }
          next[field] = nextValue
          return next
        })
      )
      return
    }
    const created: PricedChannelModel = { model }
    created[field] = nextValue
    props.onChange([...value, created])
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
  const handleDraftChange = (next: string | null) => {
    const model = (next ?? '').trim()
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
    if (expandedModel === model) setExpandedModel(null)
    props.onChange(value.filter((item) => item.model !== model))
  }

  const expandedRow = expandedModel
    ? (value.find((item) => item.model === expandedModel) ?? {
        model: expandedModel,
      })
    : null

  return (
    <div className='space-y-3'>
      {!props.disabled && (
        <div className='flex flex-wrap items-center gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            aria-expanded={importOpen}
            onClick={() => setImportOpen((open) => !open)}
          >
            <ClipboardPaste className='size-3.5' aria-hidden='true' />
            {t('Bulk paste import')}
          </Button>
        </div>
      )}

      {importOpen && !props.disabled && (
        <ChannelPriceImportPanel
          value={value}
          onApply={(next) => props.onChange(next)}
          onClose={() => setImportOpen(false)}
        />
      )}

      {rows.length > 0 && (
        <div className='overflow-x-auto'>
          <table className='w-full min-w-[820px] text-sm'>
            <thead>
              <tr className='text-muted-foreground border-b text-left'>
                <th className='py-2 pr-2 font-medium'>{t('Model')}</th>
                <th className='py-2 pr-2 font-medium'>{t('Input')}</th>
                <th className='py-2 pr-2 font-medium'>{t('Output')}</th>
                <th className='py-2 pr-2 font-medium'>{t('Cache read')}</th>
                <th className='py-2 pr-2 font-medium'>{t('Cache write')}</th>
                <th className='py-2 pr-2 font-medium'>{t('Current price')}</th>
                <th className='py-2 pr-2 font-medium'>{t('Bands')}</th>
                <th className='w-8 py-2' aria-hidden='true' />
              </tr>
            </thead>
            <tbody>
              {rows.map((model) => {
                const row =
                  value.find((item) => item.model === model) ??
                  ({ model } as PricedChannelModel)
                const effective = effectivePriceAt(row, now)
                const bandCount = row.bands?.length ?? 0
                return (
                  <tr key={model} className='border-b last:border-0'>
                    <td className='py-1.5 pr-2 break-all'>{model}</td>
                    {PRICE_FIELDS.map((field) => (
                      <td key={field} className='py-1.5 pr-2'>
                        <PriceNumberInput
                          label={`${model} ${field}`}
                          value={row[field]}
                          disabled={props.disabled}
                          onChange={(next) => setPrice(model, field, next)}
                        />
                      </td>
                    ))}
                    <td className='py-1.5 pr-2'>
                      <div className='text-xs whitespace-nowrap tabular-nums'>
                        {t('Input')} {effective.input} · {t('Output')}{' '}
                        {effective.output}
                      </div>
                      <div className='text-muted-foreground text-[11px] whitespace-nowrap tabular-nums'>
                        {t('Cache read')} {effective.cache_read} ·{' '}
                        {t('Cache write')} {effective.cache_write}
                      </div>
                      <div className='text-muted-foreground text-[11px] whitespace-nowrap'>
                        {t('price_band {{index}}', {
                          index: effective.bandIndex,
                        })}
                      </div>
                    </td>
                    <td className='py-1.5 pr-2'>
                      <Button
                        type='button'
                        variant={
                          expandedModel === model ? 'secondary' : 'outline'
                        }
                        size='sm'
                        aria-expanded={expandedModel === model}
                        aria-label={t(
                          'Edit peak/off-peak bands for {{model}}',
                          { model }
                        )}
                        disabled={props.disabled}
                        onClick={() =>
                          setExpandedModel((current) =>
                            current === model ? null : model
                          )
                        }
                      >
                        {bandCount > 0
                          ? t('Bands: {{n}}', { n: bandCount })
                          : t('All day')}
                      </Button>
                    </td>
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

      {expandedRow && (
        <ChannelPriceBandsEditor
          model={expandedRow.model}
          row={expandedRow}
          now={now}
          disabled={props.disabled}
          onClose={() => setExpandedModel(null)}
          onTimezoneChange={(timezone) =>
            patchRow(expandedRow.model, { timezone })
          }
          onBandsChange={(bands) => patchRow(expandedRow.model, { bands })}
        />
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
