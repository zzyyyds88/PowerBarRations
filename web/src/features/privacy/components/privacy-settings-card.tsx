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
import { Plus, Save, ShieldCheck, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { TitledCard } from '@/components/ui/titled-card'
import { formatTokens } from '@/lib/format'
import { cn } from '@/lib/utils'

import type {
  PrivacyModelStatus,
  PrivacyRule,
  PrivacySettings,
} from '../privacy-api'

const KIND_LABELS: Record<string, string> = {
  ssn: 'SSN',
  passport: 'Passport',
  iban: 'IBAN',
  birthday: 'Birthday',
  mac: 'MAC address',
  serial: 'Serial number',
  home: 'Home folder user',
  userhost: 'user@host',
  ip: 'Public IP',
  bucket: 'Cloud bucket',
}

const ACTION_KINDS = [
  'API_KEY',
  'PRIVATE_KEY',
  'TOKEN',
  'PASSWORD',
  'SECRET',
  'EMAIL',
  'PHONE',
  'ID_CARD',
  'BANK_CARD',
  'SSN',
  'PASSPORT',
  'IBAN',
  'USERNAME',
  'HOSTNAME',
  'IP',
  'BUCKET',
]

function SwitchRow(props: {
  label: string
  description: string
  checked: boolean
  onChange: (checked: boolean) => void
  testId?: string
}) {
  return (
    <label className='flex items-start justify-between gap-4 rounded-md border px-3 py-2.5'>
      <span className='min-w-0'>
        <span className='block text-sm font-medium'>{props.label}</span>
        <span className='text-muted-foreground mt-0.5 block text-xs'>
          {props.description}
        </span>
      </span>
      <input
        type='checkbox'
        data-privacy-toggle={props.testId}
        className='accent-primary mt-0.5 size-4 shrink-0'
        checked={props.checked}
        onChange={(event) => props.onChange(event.target.checked)}
      />
    </label>
  )
}

function actionLabel(value: string, t: (key: string) => string): string {
  if (value === 'warn') return t('Warn')
  if (value === 'block') return t('Block')
  return t('Mask')
}

function modelStateLabel(
  installed: boolean | undefined,
  downloading: boolean | undefined,
  t: (key: string) => string,
  rulesOnly = false
): string {
  if (installed) return t('Downloaded')
  if (downloading) return t('Downloading…')
  return rulesOnly ? t('Rules only') : t('Not downloaded')
}

function linesOf(values: string[]): string {
  return values.join('\n')
}

function parseLines(value: string): string[] {
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
}

export function PrivacySettingsCard(props: {
  settings: PrivacySettings
  onChange: (next: PrivacySettings) => void
  onSave: () => void
  saving: boolean
  /** 只重置检测口径（kinds/rules/words/actions/allow/shape/min_score），不动总开关。 */
  onApplyRecommended: () => void
  applyingRecommended: boolean
  model: PrivacyModelStatus | undefined
  onDownload: () => void
  downloading: boolean
}) {
  const { t } = useTranslation()
  const s = props.settings
  const patch = (part: Partial<PrivacySettings>) =>
    props.onChange({ ...s, ...part })
  const patchModel = (part: Partial<PrivacySettings['model']>) =>
    props.onChange({ ...s, model: { ...s.model, ...part } })
  const setRule = (index: number, part: Partial<PrivacyRule>) => {
    const rules = s.rules.map((rule, i) =>
      i === index ? { ...rule, ...part } : rule
    )
    patch({ rules })
  }

  return (
    <TitledCard
      title={t('Privacy masking')}
      description={t(
        'Secrets and personal data leave as placeholders and come back in the reply.'
      )}
      icon={<ShieldCheck className='size-4' aria-hidden='true' />}
      iconTone='success'
      action={
        <div className='flex items-center gap-2'>
          <Button
            type='button'
            size='sm'
            variant='outline'
            className='h-8'
            onClick={props.onApplyRecommended}
            disabled={props.applyingRecommended || props.saving}
          >
            {props.applyingRecommended
              ? t('Applying…')
              : t('Apply recommended')}
          </Button>
          <Button
            type='button'
            size='sm'
            className='h-8'
            onClick={props.onSave}
            disabled={props.saving}
          >
            <Save className='size-3.5' aria-hidden='true' />
            {props.saving ? t('Saving…') : t('Save')}
          </Button>
        </div>
      }
      contentClassName='space-y-4 p-4'
    >
      <div className='grid gap-2 sm:grid-cols-3'>
        <SwitchRow
          label={t('Secrets')}
          description={t('API keys, tokens, private keys and passwords.')}
          checked={s.secrets}
          onChange={(secrets) => patch({ secrets })}
          testId='secrets'
        />
        <SwitchRow
          label={t('Personal data')}
          description={t('Emails, phone numbers, IDs, bank cards and more.')}
          checked={s.personal}
          onChange={(personal) => patch({ personal })}
          testId='personal'
        />
        <SwitchRow
          label={t('Local model')}
          description={t(
            'AstrLink Guard finds names and addresses in free text.'
          )}
          checked={s.model.enabled}
          onChange={(enabled) =>
            patchModel({
              enabled,
              required: enabled ? s.model.required : false,
            })
          }
          testId='model'
        />
      </div>

      <div className='rounded-md border p-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div>
            <div className='text-sm font-medium'>{t('Local model status')}</div>
            <div className='text-muted-foreground text-xs'>
              {props.model?.name ?? 'AstrLink Guard'} ·{' '}
              {formatTokens(props.model?.size ?? 0)} ·{' '}
              {modelStateLabel(
                props.model?.installed,
                props.model?.downloading,
                t
              )}
            </div>
          </div>
          <Button
            type='button'
            variant='outline'
            size='sm'
            className='h-8'
            onClick={props.onDownload}
            disabled={
              props.downloading || props.model?.installed || !s.model.enabled
            }
          >
            {modelStateLabel(
              props.model?.installed,
              props.model?.downloading,
              t,
              true
            )}
          </Button>
        </div>
        {props.model?.downloading && (
          <div className='bg-muted mt-2 h-1.5 overflow-hidden rounded-full'>
            <div
              className='bg-primary h-full'
              style={{
                width: `${Math.min(100, ((props.model.done || 0) / Math.max(1, props.model.total)) * 100)}%`,
              }}
            />
          </div>
        )}
        {props.model?.error && (
          <p className='text-destructive mt-2 text-xs'>{props.model.error}</p>
        )}
        <div className='mt-2 flex flex-wrap items-center gap-4 text-xs'>
          <label className='flex items-center gap-1.5'>
            <input
              type='checkbox'
              className='accent-primary size-3.5'
              checked={s.model.required}
              disabled={!s.model.enabled}
              onChange={(event) =>
                patchModel({ required: event.target.checked })
              }
            />
            {t('Block requests when the model is unavailable')}
          </label>
          <label className='flex items-center gap-1.5'>
            {t('Confidence')}
            <Input
              type='number'
              min={0.5}
              max={0.99}
              step={0.01}
              className='h-7 w-20'
              value={s.model.min_score}
              onChange={(event) =>
                patchModel({ min_score: Number(event.target.value) })
              }
            />
          </label>
        </div>
      </div>

      <div>
        <div className='mb-2 text-sm font-medium'>
          {t('Personal data kinds')}
        </div>
        <div className='grid gap-1.5 sm:grid-cols-2 lg:grid-cols-3'>
          {Object.entries(KIND_LABELS).map(([id, label]) => (
            <label key={id} className='flex items-center gap-2 text-xs'>
              <Checkbox
                checked={s.kinds[id] ?? false}
                onCheckedChange={(checked) =>
                  patch({ kinds: { ...s.kinds, [id]: Boolean(checked) } })
                }
              />
              {t(label)}
            </label>
          ))}
        </div>
      </div>

      <div className='grid gap-3 lg:grid-cols-2'>
        <div>
          <Label className='text-xs'>{t('Never mask these values')}</Label>
          <Textarea
            className='mt-1 min-h-20 font-mono text-xs'
            placeholder={t('One value per line')}
            value={linesOf(s.allow)}
            onChange={(event) =>
              patch({ allow: parseLines(event.target.value) })
            }
          />
        </div>
        <div>
          <Label className='text-xs'>{t('Your own words')}</Label>
          <Textarea
            className='mt-1 min-h-20 font-mono text-xs'
            placeholder={t('One word per line')}
            value={linesOf(s.words)}
            onChange={(event) =>
              patch({ words: parseLines(event.target.value) })
            }
          />
          <div className='mt-1.5 flex flex-wrap gap-3 text-xs'>
            <label className='flex items-center gap-1.5'>
              <input
                type='checkbox'
                className='accent-primary size-3.5'
                checked={s.words_any_case}
                onChange={(event) =>
                  patch({ words_any_case: event.target.checked })
                }
              />
              {t('Any case')}
            </label>
            <label className='flex items-center gap-1.5'>
              <input
                type='checkbox'
                className='accent-primary size-3.5'
                checked={s.words_whole}
                onChange={(event) =>
                  patch({ words_whole: event.target.checked })
                }
              />
              {t('Whole words only')}
            </label>
          </div>
        </div>
      </div>

      <div>
        <div className='mb-1 flex items-center justify-between gap-2'>
          <div className='text-sm font-medium'>{t('Your own rules')}</div>
          <Button
            type='button'
            variant='outline'
            size='sm'
            className='h-7 px-2 text-xs'
            onClick={() =>
              patch({
                rules: [...s.rules, { kind: 'CUSTOM', prefix: '' }],
              })
            }
          >
            <Plus className='size-3' aria-hidden='true' />
            {t('Add rule')}
          </Button>
        </div>
        <div className='space-y-1.5'>
          {s.rules.map((rule, index) => (
            <div
              key={`${rule.kind}-${rule.prefix ?? ''}-${rule.regex ?? ''}`}
              className='grid gap-1.5 sm:grid-cols-[1fr_1fr_1fr_auto]'
            >
              <Input
                className='h-8 font-mono text-xs'
                placeholder={t('Kind')}
                value={rule.kind}
                onChange={(event) =>
                  setRule(index, { kind: event.target.value })
                }
              />
              <Input
                className='h-8 font-mono text-xs'
                placeholder={t('Prefix')}
                value={rule.prefix ?? ''}
                onChange={(event) =>
                  setRule(index, {
                    prefix: event.target.value,
                    regex: '',
                  })
                }
              />
              <Input
                className='h-8 font-mono text-xs'
                placeholder={t('Regex (RE2)')}
                value={rule.regex ?? ''}
                onChange={(event) =>
                  setRule(index, {
                    regex: event.target.value,
                    prefix: '',
                  })
                }
              />
              <Button
                type='button'
                variant='ghost'
                size='sm'
                className='h-8 px-2'
                aria-label={t('Remove rule')}
                onClick={() =>
                  patch({ rules: s.rules.filter((_, i) => i !== index) })
                }
              >
                <Trash2 className='size-3.5' aria-hidden='true' />
              </Button>
            </div>
          ))}
          {s.rules.length === 0 && (
            <p className='text-muted-foreground text-xs'>
              {t('No custom rules.')}
            </p>
          )}
        </div>
      </div>

      <div>
        <div className='mb-2 text-sm font-medium'>{t('Actions')}</div>
        <div className='grid gap-1.5 sm:grid-cols-2 lg:grid-cols-4'>
          {ACTION_KINDS.map((kind) => (
            <label key={kind} className='flex items-center gap-2 text-xs'>
              <span className='w-24 shrink-0 font-mono'>{kind}</span>
              <Select
                value={s.actions[kind] ?? 'mask'}
                onValueChange={(value) => {
                  const actions = { ...s.actions }
                  if (!value || value === 'mask') {
                    delete actions[kind]
                  } else {
                    actions[kind] = String(value)
                  }
                  patch({ actions })
                }}
              >
                <SelectTrigger className='h-7 w-24 text-xs'>
                  <SelectValue>
                    {actionLabel(s.actions[kind] ?? 'mask', t)}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='mask'>{t('Mask')}</SelectItem>
                  <SelectItem value='warn'>{t('Warn')}</SelectItem>
                  <SelectItem value='block'>{t('Block')}</SelectItem>
                </SelectContent>
              </Select>
            </label>
          ))}
        </div>
      </div>

      <label className={cn('flex items-center gap-2 text-xs')}>
        <input
          type='checkbox'
          className='accent-primary size-3.5'
          checked={s.shape}
          onChange={(event) => patch({ shape: event.target.checked })}
        />
        {t(
          'Send keys as made-up keys of the same shape instead of placeholders'
        )}
      </label>
    </TitledCard>
  )
}
