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
import { Plus, Trash2 } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import {
  DEFAULT_PRICE_TIMEZONE,
  MAX_PRICE_BANDS,
  PRICE_FIELD_LABEL_KEYS,
  PRICE_FIELDS,
  WEEK_DAYS,
  buildDeepSeekPeakPreset,
  effectivePriceAt,
  formatClockHHMM,
  parseClockHHMM,
  priceRowIssues,
  type BandIssue,
  type PriceBand,
  type PriceField,
  type PricedChannelModel,
} from '../lib/channel-prices'
import { PriceNumberInput } from './channel-price-number-input'
import { ChannelPriceWeekGrid } from './channel-price-week-grid'

// 单行价格的峰谷编辑面板（ui-spec §9）：
// - timezone（默认 Asia/Shanghai，IANA 名）；
// - 有序 band 列表：days 多选（周一=1）、start/end（HH:MM，左闭右开）；
// - 当前生效价预览（按浏览器当前时间匹配第一条 band，展示与日志
//   `price_band` 一致的下标，-1 = 行级兜底）；
// - 周视图网格与就地校验；
// - DeepSeek 官方峰谷预设（只填结构，不写死金额）。

const WEEK_DAY_KEYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'] as const

function bandIssueMessage(t: TFunction, issue: BandIssue): string {
  switch (issue) {
    case 'days_empty':
      return t('Select at least one weekday')
    case 'day_out_of_range':
      return t('Weekdays must be 1–7 (Monday = 1), e.g. 1-5 or 1;3;5')
    case 'start_invalid':
    case 'end_invalid':
      return t('Use HH:MM (00:00–23:59)')
    case 'start_equals_end':
      return t('Start and end must differ')
    case 'negative_price':
      return t('Negative prices are not allowed')
    default:
      return ''
  }
}

/**
 * band 没有服务端 id，用客户端 key 维持行身份：编辑时段内容不能改变 key，
 * 否则受控输入会随每次键入重挂载、焦点丢失。长度变化（添加/删除/预设/导入）
 * 在渲染期对齐。
 */
let bandKeySequence = 0

function createBandKey(): string {
  // 不用 crypto.randomUUID：它在非安全上下文（局域网明文 HTTP）不可用。
  bandKeySequence += 1
  return `band-${bandKeySequence}`
}

function resizeBandKeys(keys: string[], length: number): string[] {
  if (keys.length === length) return keys
  if (keys.length > length) return keys.slice(0, length)
  const next = [...keys]
  while (next.length < length) next.push(createBandKey())
  return next
}

function isCrossMidnight(band: PriceBand): boolean {
  const start = parseClockHHMM(band.start)
  const end = parseClockHHMM(band.end)
  return start !== null && end !== null && start > end
}

type ChannelPriceBandsEditorProps = {
  model: string
  row: PricedChannelModel
  now: Date
  disabled?: boolean
  onClose: () => void
  onTimezoneChange: (timezone: string) => void
  onBandsChange: (bands: PriceBand[]) => void
}

export function ChannelPriceBandsEditor(props: ChannelPriceBandsEditorProps) {
  const { t } = useTranslation()
  const timezoneId = useId()
  const [presetOpen, setPresetOpen] = useState(false)
  const bands = props.row.bands ?? []
  const [bandKeys, setBandKeys] = useState<string[]>(() =>
    bands.map(() => createBandKey())
  )
  const [trackedBandCount, setTrackedBandCount] = useState(bands.length)
  if (trackedBandCount !== bands.length) {
    // 外部替换（导入/表单回填/预设）后对齐 key 数量；React 会在提交前重渲染。
    setTrackedBandCount(bands.length)
    setBandKeys((previous) => resizeBandKeys(previous, bands.length))
  }
  const rowIssues = priceRowIssues(props.row)
  const effective = effectivePriceAt(props.row, props.now)
  const timezone = props.row.timezone?.trim() || DEFAULT_PRICE_TIMEZONE
  const timezoneInvalid = rowIssues.row.includes('timezone_invalid')
  const tooManyBands = rowIssues.row.includes('too_many_bands')

  const updateBand = (index: number, patch: Partial<PriceBand>) => {
    props.onBandsChange(
      bands.map((band, bandIndex) =>
        bandIndex === index ? { ...band, ...patch } : band
      )
    )
  }

  const updateBandPrice = (
    index: number,
    field: PriceField,
    value: number | undefined
  ) => {
    props.onBandsChange(
      bands.map((band, bandIndex) => {
        if (bandIndex !== index) return band
        const next: PriceBand = { ...band }
        next[field] = value
        return next
      })
    )
  }

  const toggleDay = (index: number, day: number) => {
    const band = bands[index]
    const days = band.days.includes(day)
      ? band.days.filter((item) => item !== day)
      : [...band.days, day].sort((a, b) => a - b)
    updateBand(index, { days })
  }

  const addBand = () => {
    setBandKeys((previous) => [...previous, createBandKey()])
    props.onBandsChange([
      ...bands,
      { days: [...WEEK_DAYS], start: '09:00', end: '18:00' },
    ])
  }

  const removeBand = (index: number) => {
    setBandKeys((previous) =>
      previous.filter((_, keyIndex) => keyIndex !== index)
    )
    props.onBandsChange(bands.filter((_, bandIndex) => bandIndex !== index))
  }

  const applyPreset = () => {
    setBandKeys(buildDeepSeekPeakPreset().map(() => createBandKey()))
    props.onBandsChange(buildDeepSeekPeakPreset())
    setPresetOpen(false)
  }

  return (
    <section
      aria-label={t('Peak/off-peak bands')}
      className='bg-card/40 space-y-3 rounded-lg border p-3'
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex min-w-0 flex-wrap items-center gap-2'>
          <h3 className='text-sm font-medium'>{t('Peak/off-peak bands')}</h3>
          <Badge variant='secondary' className='max-w-full truncate'>
            {props.model}
          </Badge>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={() => {
              if (bands.length === 0) {
                applyPreset()
                return
              }
              setPresetOpen(true)
            }}
          >
            {t('DeepSeek peak/off-peak preset')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={props.onClose}
          >
            {t('Close')}
          </Button>
        </div>
      </div>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Fills the band structure only. Set the prices from your purchase cost.'
        )}
      </p>

      <div className='space-y-1'>
        <Label htmlFor={timezoneId}>{t('Time zone')}</Label>
        <Input
          id={timezoneId}
          value={props.row.timezone ?? ''}
          placeholder={DEFAULT_PRICE_TIMEZONE}
          disabled={props.disabled}
          aria-invalid={timezoneInvalid}
          onChange={(event) => props.onTimezoneChange(event.target.value)}
        />
        <p className='text-muted-foreground text-xs'>
          {t('IANA time zone, e.g. Asia/Shanghai')}
        </p>
        {timezoneInvalid && (
          <p role='alert' className='text-destructive text-xs'>
            {t('Invalid time zone')}
          </p>
        )}
      </div>

      <div className='bg-muted/40 space-y-1 rounded-lg p-2'>
        {effective.clock && (
          <p className='text-xs'>
            {t('Now {{time}} · {{timezone}}', {
              time: formatClockHHMM(effective.clock.minutes),
              timezone,
            })}
          </p>
        )}
        <p className='text-sm tabular-nums'>
          {t('Input')} {effective.input} · {t('Output')} {effective.output} ·{' '}
          {t('Cache read')} {effective.cache_read} · {t('Cache write')}{' '}
          {effective.cache_write}
        </p>
        <p className='text-xs'>
          {t('price_band {{index}}', { index: effective.bandIndex })}
          {effective.bandIndex < 0
            ? ` · ${t('Fallback (row price)')}`
            : ` · ${t('Band {{index}}', { index: effective.bandIndex + 1 })}`}
        </p>
      </div>

      <div className='space-y-2'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <span className='text-sm font-medium'>{t('Bands')}</span>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled || bands.length >= MAX_PRICE_BANDS}
            onClick={addBand}
          >
            <Plus className='size-3.5' aria-hidden='true' />
            {t('Add band')}
          </Button>
        </div>
        {bands.length === 0 && (
          <p className='text-muted-foreground text-sm'>
            {t('No bands: the row price applies all day.')}
          </p>
        )}
        {tooManyBands && (
          <p role='alert' className='text-destructive text-sm'>
            {t('A row can hold at most 64 bands.')}
          </p>
        )}
        {bands.map((band, index) => (
          <div
            key={bandKeys[index] ?? `band-${index}`}
            className='space-y-2 rounded-lg border p-2'
          >
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <span className='text-sm font-medium'>
                {t('Band {{index}}', { index: index + 1 })}
              </span>
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                aria-label={t('Remove band {{index}}', { index: index + 1 })}
                disabled={props.disabled}
                onClick={() => removeBand(index)}
              >
                <Trash2 className='size-3.5' aria-hidden='true' />
              </Button>
            </div>

            <div
              role='group'
              aria-label={t('Days')}
              className='flex flex-wrap gap-1'
            >
              {WEEK_DAYS.map((day, dayIndex) => (
                <Button
                  key={day}
                  type='button'
                  size='sm'
                  variant={band.days.includes(day) ? 'default' : 'outline'}
                  aria-pressed={band.days.includes(day)}
                  disabled={props.disabled}
                  onClick={() => toggleDay(index, day)}
                >
                  {t(WEEK_DAY_KEYS[dayIndex])}
                </Button>
              ))}
            </div>

            <div className='grid grid-cols-2 gap-2'>
              <div className='space-y-1'>
                <Label htmlFor={`${timezoneId}-start-${index}`}>
                  {t('Start')}
                </Label>
                <Input
                  id={`${timezoneId}-start-${index}`}
                  aria-label={t('Band {{index}} start', { index: index + 1 })}
                  placeholder='00:00'
                  value={band.start}
                  disabled={props.disabled}
                  onChange={(event) =>
                    updateBand(index, { start: event.target.value })
                  }
                />
              </div>
              <div className='space-y-1'>
                <Label htmlFor={`${timezoneId}-end-${index}`}>{t('End')}</Label>
                <Input
                  id={`${timezoneId}-end-${index}`}
                  aria-label={t('Band {{index}} end', { index: index + 1 })}
                  placeholder='00:00'
                  value={band.end}
                  disabled={props.disabled}
                  onChange={(event) =>
                    updateBand(index, { end: event.target.value })
                  }
                />
              </div>
            </div>

            {isCrossMidnight(band) && (
              <p className='text-xs text-amber-600 dark:text-amber-500'>
                {t(
                  'Spanning midnight: the band belongs to its start day and ends the next day.'
                )}
              </p>
            )}

            <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
              {PRICE_FIELDS.map((field) => (
                <div key={field} className='space-y-1'>
                  <span className='text-muted-foreground text-xs'>
                    {t(PRICE_FIELD_LABEL_KEYS[field])}
                  </span>
                  <PriceNumberInput
                    label={t('Band {{index}} {{field}} price', {
                      index: index + 1,
                      field: t(PRICE_FIELD_LABEL_KEYS[field]),
                    })}
                    value={band[field]}
                    disabled={props.disabled}
                    onChange={(value) => updateBandPrice(index, field, value)}
                  />
                </div>
              ))}
            </div>
            <p className='text-muted-foreground text-xs'>
              {t('Blank inherits the row price; 0 means free for that slot.')}
            </p>

            {[
              ...new Set(
                (rowIssues.bands[index] ?? []).map((issue) =>
                  bandIssueMessage(t, issue)
                )
              ),
            ].map((message) => (
              <p
                key={message}
                role='alert'
                className='text-destructive text-xs'
              >
                {message}
              </p>
            ))}
          </div>
        ))}
      </div>

      <div className='space-y-1'>
        <span className='text-muted-foreground text-xs'>
          {t('Weekly coverage')}
        </span>
        <ChannelPriceWeekGrid bands={bands} />
      </div>

      <ConfirmDialog
        open={presetOpen}
        onOpenChange={setPresetOpen}
        title={t('Replace the current bands with the preset?')}
        desc={t(
          'The preset fills two daily bands (00:30–08:30 and the rest of the day) for all seven days and leaves every price blank.'
        )}
        confirmText={t('DeepSeek peak/off-peak preset')}
        handleConfirm={applyPreset}
      />
    </section>
  )
}
