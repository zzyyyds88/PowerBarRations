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
import { Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import type { ComboboxInputOption } from '@/components/ui/combobox-input'
import { Input } from '@/components/ui/input'

import type { ChannelRateLimit } from '../types'

// 渠道出站准入编辑器（ADR 0012、routing-spec §3.2）：
// 按渠道声明的模型名配置并发/RPM 与有界排队；默认不限制（两个上限 0）。
// max_concurrency = -1 表示自适应 AIMD（上限由运行态学习）。
// queue_limit=0 用默认 32；queue_wait_seconds=0 表示不排队（不就绪即 429）。

type RateLimitNumberInputProps = {
  label: string
  value: number | undefined
  disabled?: boolean
  min?: number
  allowNegativeOne?: boolean
  onChange: (value: number | undefined) => void
}

function RateLimitNumberInput(props: RateLimitNumberInputProps) {
  const [draft, setDraft] = useState<string | null>(null)
  const format = (value?: number) =>
    value === undefined || value === 0 ? '' : String(value)
  return (
    <Input
      aria-label={props.label}
      inputMode='numeric'
      placeholder={props.allowNegativeOne ? '0 / -1' : '0'}
      value={draft ?? format(props.value)}
      disabled={props.disabled}
      onChange={(event) => {
        setDraft(event.target.value)
        const trimmed = event.target.value.trim()
        if (trimmed === '') {
          props.onChange(0)
          return
        }
        const parsed = Number(trimmed)
        if (!Number.isFinite(parsed)) return
        const min = props.min ?? 0
        if (parsed < min || !Number.isInteger(parsed)) return
        props.onChange(parsed)
      }}
      onBlur={() => setDraft(null)}
    />
  )
}

type ChannelRateLimitsEditorProps = {
  value: ChannelRateLimit[]
  models: string[]
  onChange: (next: ChannelRateLimit[]) => void
  disabled?: boolean
}

export function ChannelRateLimitsEditor(props: ChannelRateLimitsEditorProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState('')

  const addOptions = useMemo<ComboboxInputOption[]>(
    () =>
      props.models
        .filter((model) => !props.value.some((item) => item.model === model))
        .map((model) => ({ value: model, label: model })),
    [props.models, props.value]
  )

  const addRow = (raw: string) => {
    const model = raw.trim()
    if (!model || props.value.some((item) => item.model === model)) {
      setDraft('')
      return
    }
    props.onChange([
      ...props.value,
      {
        model,
        max_concurrency: 0,
        max_rpm: 0,
        queue_limit: 0,
        queue_wait_seconds: 0,
      },
    ])
    setDraft('')
  }

  const patchRow = (model: string, patch: Partial<ChannelRateLimit>) => {
    props.onChange(
      props.value.map((item) =>
        item.model === model ? { ...item, ...patch } : item
      )
    )
  }

  return (
    <div className='space-y-3'>
      {props.value.length > 0 && (
        <div className='overflow-x-auto'>
          <table className='w-full min-w-[760px] text-sm'>
            <thead>
              <tr className='text-muted-foreground border-b text-left'>
                <th className='py-2 pr-2 font-medium'>{t('Model')}</th>
                <th className='py-2 pr-2 font-medium'>
                  {t('Max concurrency')}
                </th>
                <th className='py-2 pr-2 font-medium'>{t('Max RPM')}</th>
                <th className='py-2 pr-2 font-medium'>{t('Queue limit')}</th>
                <th className='py-2 pr-2 font-medium'>
                  {t('Queue wait (seconds)')}
                </th>
                <th className='w-8 py-2' aria-hidden='true' />
              </tr>
            </thead>
            <tbody>
              {props.value.map((item) => (
                <tr key={item.model} className='border-b last:border-0'>
                  <td className='py-1.5 pr-2 break-all'>{item.model}</td>
                  <td className='py-1.5 pr-2'>
                    <RateLimitNumberInput
                      label={`${item.model} ${t('Max concurrency')}`}
                      value={item.max_concurrency}
                      min={-1}
                      allowNegativeOne
                      disabled={props.disabled}
                      onChange={(next) =>
                        patchRow(item.model, { max_concurrency: next ?? 0 })
                      }
                    />
                  </td>
                  <td className='py-1.5 pr-2'>
                    <RateLimitNumberInput
                      label={`${item.model} ${t('Max RPM')}`}
                      value={item.max_rpm}
                      disabled={props.disabled}
                      onChange={(next) =>
                        patchRow(item.model, { max_rpm: next ?? 0 })
                      }
                    />
                  </td>
                  <td className='py-1.5 pr-2'>
                    <RateLimitNumberInput
                      label={`${item.model} ${t('Queue limit')}`}
                      value={item.queue_limit}
                      disabled={props.disabled}
                      onChange={(next) =>
                        patchRow(item.model, { queue_limit: next ?? 0 })
                      }
                    />
                  </td>
                  <td className='py-1.5 pr-2'>
                    <RateLimitNumberInput
                      label={`${item.model} ${t('Queue wait (seconds)')}`}
                      value={item.queue_wait_seconds}
                      disabled={props.disabled}
                      onChange={(next) =>
                        patchRow(item.model, { queue_wait_seconds: next ?? 0 })
                      }
                    />
                  </td>
                  <td className='py-1.5'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      aria-label={t('Remove model')}
                      disabled={props.disabled}
                      onClick={() =>
                        props.onChange(
                          props.value.filter(
                            (entry) => entry.model !== item.model
                          )
                        )
                      }
                    >
                      <Trash2 className='size-4' aria-hidden='true' />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {props.value.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'No concurrency limits configured. Requests are sent upstream without local admission control.'
          )}
        </p>
      )}

      {!props.disabled && (
        <Combobox
          options={addOptions}
          allowCustomValue
          value={draft}
          placeholder={t('Select from channel models or type a model name')}
          aria-label={t('Add a model to limit')}
          onValueChange={(next) => {
            const model = (next ?? '').trim()
            if (model && addOptions.some((option) => option.value === model)) {
              addRow(model)
              return
            }
            setDraft(model)
          }}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              addRow(draft)
            }
          }}
        />
      )}
      <p className='text-muted-foreground text-xs'>
        {t(
          '0 = unlimited. max_concurrency -1 enables adaptive AIMD; queue_limit 0 uses 32; queue_wait_seconds 0 disables queueing.'
        )}
      </p>
    </div>
  )
}
