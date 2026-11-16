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
import type { TFunction } from 'i18next'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import {
  DEFAULT_PRICE_TIMEZONE,
  PRICE_FIELD_LABEL_KEYS,
  PRICE_FIELDS,
  type PriceBand,
  type PricedChannelModel,
} from '../lib/channel-prices'
import {
  formatWeekdayList,
  mergeImportedPrices,
  parsePriceImport,
  type PriceImportIssue,
  type PriceImportResult,
} from '../lib/channel-prices-import'
import type { ChannelModelPrice } from '../types'

// 批量粘贴导入（ui-spec §9）：JSON 数组或固定列序 CSV →
// 解析 → 表格预览 → 确认后写回表单（最终随渠道 PUT 提交）。
// 解析失败逐行给出行号与原因；有错误时不提供写入动作，避免半截导入。

function importIssueReason(t: TFunction, issue: PriceImportIssue): string {
  switch (issue.code) {
    case 'invalid_json':
      return t('Invalid JSON')
    case 'not_array':
      return t('Expected a JSON array of price rows')
    case 'invalid_row':
      return t('Invalid price row')
    case 'csv_columns':
      return t('CSV rows must have 5 to 9 columns')
    case 'empty_model':
      return t('Price rows need a model name')
    case 'empty_row':
      return t('Empty price row')
    case 'duplicate_fallback':
      return t('Duplicate all-day row for {{model}}', {
        model: issue.detail ?? '',
      })
    case 'duplicate_model':
      return t('Duplicate price row for {{model}}', {
        model: issue.detail ?? '',
      })
    case 'too_many_bands':
      return t('A row can hold at most 64 bands.')
    case 'invalid_timezone':
      return t('Invalid time zone')
    case 'days_empty':
      return t('Select at least one weekday')
    case 'days_invalid':
      return t('Weekdays must be 1–7 (Monday = 1), e.g. 1-5 or 1;3;5')
    case 'time_invalid':
      return t('Use HH:MM (00:00–23:59)')
    case 'time_equal':
      return t('Start and end must differ')
    case 'negative_price':
      return t('Negative prices are not allowed')
    default:
      return ''
  }
}

function bandSummary(band: PriceBand, t: TFunction): string {
  const window = `${formatWeekdayList(band.days)} ${band.start}–${band.end}`
  const prices = PRICE_FIELDS.filter((field) => band[field] !== undefined).map(
    (field) => `${t(PRICE_FIELD_LABEL_KEYS[field])} ${band[field]}`
  )
  return prices.length === 0 ? window : `${window} (${prices.join(' / ')})`
}

type ChannelPriceImportPanelProps = {
  value: ChannelModelPrice[]
  disabled?: boolean
  onApply: (next: PricedChannelModel[]) => void
  onClose: () => void
}

export function ChannelPriceImportPanel(props: ChannelPriceImportPanelProps) {
  const { t } = useTranslation()
  const textareaId = useId()
  const [text, setText] = useState('')
  const [parsed, setParsed] = useState<{
    source: string
    result: PriceImportResult
  } | null>(null)
  const [appliedCount, setAppliedCount] = useState<number | null>(null)

  const result = parsed?.source === text ? parsed.result : null
  const canApply =
    result !== null && result.issues.length === 0 && result.rows.length > 0

  return (
    <section
      aria-label={t('Bulk paste import')}
      className='bg-card/40 space-y-3 rounded-lg border p-3'
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <h3 className='text-sm font-medium'>{t('Bulk paste import')}</h3>
        <Button type='button' variant='ghost' size='sm' onClick={props.onClose}>
          {t('Close')}
        </Button>
      </div>
      <div className='space-y-1'>
        <Label htmlFor={textareaId}>{t('Paste price rows')}</Label>
        <Textarea
          id={textareaId}
          rows={6}
          value={text}
          disabled={props.disabled}
          placeholder='model,input,output,cache_read,cache_write,timezone,days,start,end'
          onChange={(event) => {
            setText(event.target.value)
            setAppliedCount(null)
          }}
        />
        <p className='text-muted-foreground text-xs'>
          {t(
            'Paste a JSON array, or CSV rows in the order model,input,output,cache_read,cache_write,timezone,days,start,end.'
          )}
        </p>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Rows with days, start and end define one band; a row with all three blank sets the all-day fallback price.'
          )}
        </p>
      </div>
      <Button
        type='button'
        variant='outline'
        size='sm'
        disabled={props.disabled || text.trim() === ''}
        onClick={() => {
          setParsed({ source: text, result: parsePriceImport(text) })
          setAppliedCount(null)
        }}
      >
        {t('Parse')}
      </Button>

      {result && result.issues.length > 0 && (
        <div role='alert' className='space-y-1'>
          {result.issues.map((issue) => (
            <p
              key={`${issue.line}:${issue.code}`}
              className='text-destructive text-xs'
            >
              {result.format === 'csv'
                ? t('Line {{line}}: {{reason}}', {
                    line: issue.line,
                    reason: importIssueReason(t, issue),
                  })
                : t('Row {{row}}: {{reason}}', {
                    row: issue.line,
                    reason: importIssueReason(t, issue),
                  })}
            </p>
          ))}
        </div>
      )}

      {result && result.rows.length === 0 && result.issues.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t('Nothing to import')}
        </p>
      )}

      {result && result.rows.length > 0 && (
        <div className='space-y-2'>
          <p className='text-muted-foreground text-xs'>
            {t('Price rows parsed: {{n}}', {
              n: result.rows.length,
            })}
          </p>
          <div className='overflow-x-auto'>
            <table className='w-full min-w-[640px] text-sm'>
              <thead>
                <tr className='text-muted-foreground border-b text-left'>
                  <th className='py-2 pr-2 font-medium'>{t('Model')}</th>
                  <th className='py-2 pr-2 font-medium'>{t('Input')}</th>
                  <th className='py-2 pr-2 font-medium'>{t('Output')}</th>
                  <th className='py-2 pr-2 font-medium'>{t('Cache read')}</th>
                  <th className='py-2 pr-2 font-medium'>{t('Cache write')}</th>
                  <th className='py-2 pr-2 font-medium'>{t('Time zone')}</th>
                  <th className='py-2 font-medium'>{t('Bands')}</th>
                </tr>
              </thead>
              <tbody>
                {result.rows.map((row) => (
                  <tr key={row.model} className='border-b last:border-0'>
                    <td className='py-1.5 pr-2 break-all'>{row.model}</td>
                    {PRICE_FIELDS.map((field) => (
                      <td key={field} className='py-1.5 pr-2 tabular-nums'>
                        {row[field] ?? 0}
                      </td>
                    ))}
                    <td className='py-1.5 pr-2'>
                      {row.timezone?.trim() || DEFAULT_PRICE_TIMEZONE}
                    </td>
                    <td className='text-muted-foreground py-1.5 text-xs'>
                      {row.bands && row.bands.length > 0
                        ? row.bands
                            .map((band) => bandSummary(band, t))
                            .join(' · ')
                        : t('All day')}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Button
            type='button'
            size='sm'
            disabled={!canApply || props.disabled}
            onClick={() => {
              props.onApply(mergeImportedPrices(props.value, result.rows))
              setAppliedCount(result.rows.length)
            }}
          >
            {t('Apply to form')}
          </Button>
        </div>
      )}

      {appliedCount !== null && (
        <p className='text-sm'>
          {t('Price rows imported: {{n}}', { n: appliedCount })}
        </p>
      )}
    </section>
  )
}
