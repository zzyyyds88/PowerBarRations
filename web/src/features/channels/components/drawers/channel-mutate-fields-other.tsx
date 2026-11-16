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
import { FileText, RefreshCw } from 'lucide-react'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import {
  FIELD_DESCRIPTIONS,
  FIELD_PLACEHOLDERS,
  MODEL_FETCHABLE_TYPES,
} from '../../constants'
import { useChannelMutateContext } from './channel-mutate-context'
import {
  channelConfigurationBlockClassName,
  formatUnixTime,
} from './channel-mutate-dialog-model'
import { CardHeading, SubHeading } from './channel-mutate-dialog-parts'

export function ChannelNotesFields() {
  const { configuration, form, t } = useChannelMutateContext()
  const notesFields = (
    <div
      role='group'
      aria-label={t('Internal Notes')}
      className={channelConfigurationBlockClassName(
        configuration.blocks.internalNotes,
        'flex flex-col gap-4'
      )}
    >
      <SubHeading
        title={t('Internal Notes')}
        status={configuration.blocks.internalNotes}
        icon={<FileText className='h-3.5 w-3.5' />}
        iconTone='chart-3'
      />
      <div className='grid gap-4 sm:grid-cols-2'>
        <FormField
          control={form.control}
          name='tag'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Tag')}</FormLabel>
              <FormControl>
                <Input placeholder={t(FIELD_PLACEHOLDERS.TAG)} {...field} />
              </FormControl>
              <FormDescription>{t(FIELD_DESCRIPTIONS.TAG)}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='remark'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Remark')}</FormLabel>
              <FormControl>
                <Textarea
                  placeholder={t(FIELD_PLACEHOLDERS.REMARK)}
                  rows={2}
                  {...field}
                />
              </FormControl>
              <FormDescription>{t(FIELD_DESCRIPTIONS.REMARK)}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>
    </div>
  )

  return notesFields
}

export function ChannelHttpProtocolFields() {
  const { form, sensitiveLocked, t } = useChannelMutateContext()
  const httpProtocolFields = (
    <FormField
      control={form.control}
      name='http_protocol'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('HTTP Protocol')}</FormLabel>
          <Select
            disabled={sensitiveLocked}
            items={[
              {
                value: 'auto',
                label: t('Auto'),
              },
              {
                value: 'http1',
                label: t('HTTP/1.1'),
              },
            ]}
            value={field.value || 'auto'}
            onValueChange={(value) => {
              const nextProtocol = value === 'http1' ? 'http1' : 'auto'
              field.onChange(nextProtocol)
              if (nextProtocol === 'http1') {
                form.setValue('http2_connection_shards', 1, {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
            }}
          >
            <FormControl>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
            </FormControl>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='auto'>{t('Auto')}</SelectItem>
                <SelectItem value='http1'>{t('HTTP/1.1')}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <FormDescription>
            {t(
              'Auto negotiates HTTP/2 when available. HTTP/1.1 forces multiple keep-alive connections under concurrency.'
            )}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  return httpProtocolFields
}

export function ChannelHttpShardsFields() {
  const { currentHttpProtocol, form, sensitiveLocked, t } =
    useChannelMutateContext()
  const httpShardsFields = (
    <FormField
      control={form.control}
      name='http2_connection_shards'
      render={({ field }) => {
        const http1Selected = currentHttpProtocol === 'http1'
        const shardItems = Array.from({ length: 8 }, (_, index) => {
          const value = String(index + 1)
          return { value, label: value }
        })
        return (
          <FormItem>
            <FormLabel>{t('HTTP/2 Connection Shards')}</FormLabel>
            <Select
              items={shardItems}
              value={String(field.value || 1)}
              disabled={sensitiveLocked || http1Selected}
              onValueChange={(value) => {
                field.onChange(Number(value))
              }}
            >
              <FormControl>
                <SelectTrigger disabled={sensitiveLocked || http1Selected}>
                  <SelectValue />
                </SelectTrigger>
              </FormControl>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {shardItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <FormDescription>
              {http1Selected
                ? t(
                    'HTTP/2 connection shards are unavailable when HTTP/1.1 is selected.'
                  )
                : t(
                    'Spread HTTP/2 traffic across multiple reusable connections to the same upstream origin (1-8).'
                  )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )
      }}
    />
  )

  return httpShardsFields
}

export function ChannelUpstreamModelDetectionFields() {
  const {
    configuration,
    currentType,
    form,
    sensitiveLocked,
    t,
    upstreamDetectedModelsOmittedCount,
    upstreamDetectedModelsPreview,
    upstreamModelUpdateCheckEnabled,
    upstreamUpdateMeta,
  } = useChannelMutateContext()
  const upstreamModelDetectionFields = MODEL_FETCHABLE_TYPES.has(
    currentType
  ) && (
    <div
      role='group'
      aria-label={t('Upstream Model Detection Settings')}
      className={channelConfigurationBlockClassName(
        configuration.blocks.upstreamModelDetection,
        'flex flex-col gap-4'
      )}
    >
      <CardHeading
        title={t('Upstream Model Detection Settings')}
        status={configuration.blocks.upstreamModelDetection}
        icon={<RefreshCw className='h-4 w-4' />}
        iconTone='info'
      />
      <fieldset
        disabled={sensitiveLocked}
        className='space-y-4 disabled:opacity-60'
      >
        <div className='divide-border space-y-0 divide-y border-y'>
          <FormField
            control={form.control}
            name='upstream_model_update_check_enabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between px-4 py-3'>
                <div className='space-y-0.5'>
                  <FormLabel>{t('Upstream Model Update Check')}</FormLabel>
                  <FormDescription>
                    {t('Periodically check for upstream model changes')}
                  </FormDescription>
                  <FormMessage />
                </div>
                <FormControl>
                  <Switch
                    disabled={sensitiveLocked}
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='upstream_model_update_auto_sync_enabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between px-4 py-3'>
                <div className='space-y-0.5'>
                  <FormLabel>{t('Auto Sync Upstream Models')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Automatically sync model list when upstream changes are detected'
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    disabled={
                      sensitiveLocked || !upstreamModelUpdateCheckEnabled
                    }
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />
        </div>
        <FormField
          control={form.control}
          name='upstream_model_update_ignored_models'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Ignored upstream models')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t(
                    'e.g., gpt-4.1-nano,regex:^claude-.*$,regex:^sora-.*$'
                  )}
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Comma-separated exact model names. Prefix with regex: to ignore by regular expression.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <div className='text-muted-foreground space-y-2 border-t pt-3 text-xs'>
          <div>
            <span className='text-foreground font-medium'>
              {t('Last check time')}:
            </span>{' '}
            {formatUnixTime(upstreamUpdateMeta.lastCheckTime)}
          </div>
          <div>
            <span className='text-foreground font-medium'>
              {t('Last detected addable models')}:
            </span>{' '}
            {upstreamUpdateMeta.detectedModels.length === 0 ? (
              t('None')
            ) : (
              <>
                <span className='break-all'>
                  {upstreamDetectedModelsPreview.join(', ')}
                </span>
                {upstreamDetectedModelsOmittedCount > 0 && (
                  <span className='ml-1'>
                    {t('({{total}} total, {{omit}} omitted)', {
                      total: upstreamUpdateMeta.detectedModels.length,
                      omit: upstreamDetectedModelsOmittedCount,
                    })}
                  </span>
                )}
              </>
            )}
          </div>
        </div>
      </fieldset>
    </div>
  )

  return upstreamModelDetectionFields
}
