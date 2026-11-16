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
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import { ChannelPricesEditor } from '../channel-prices-editor'
import { useChannelMutateContext } from './channel-mutate-context'

export function ChannelProxyFields() {
  const { form, t } = useChannelMutateContext()
  const proxyFields = (
    <FormField
      control={form.control}
      name='proxy'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Proxy Address')}</FormLabel>
          <FormControl>
            <Input placeholder={t('socks5://user:pass@host:port')} {...field} />
          </FormControl>
          <FormDescription>
            {t(
              'Network proxy for this channel (supports HTTP, HTTPS, SOCKS5, and SOCKS5H)'
            )}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  return proxyFields
}

export function ChannelPricesFields() {
  const { currentModelsArray, form, sensitiveLocked, t } =
    useChannelMutateContext()
  const pbrPricesFields = (
    <FormField
      control={form.control}
      name='pbr_prices'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Upstream unit prices')}</FormLabel>
          <FormControl>
            <ChannelPricesEditor
              value={field.value ?? []}
              models={currentModelsArray}
              onChange={field.onChange}
              disabled={sensitiveLocked}
            />
          </FormControl>
          <FormDescription>
            {t(
              'CNY per 1M tokens, used for cost accounting only. Models without a channel price are not converted.'
            )}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  return pbrPricesFields
}

export function ChannelSystemPromptFields() {
  const { form, t } = useChannelMutateContext()
  const systemPromptFields = (
    <FormField
      control={form.control}
      name='system_prompt'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('System Prompt')}</FormLabel>
          <FormControl>
            <Textarea
              placeholder={t(
                'Enter system prompt (user prompt takes priority)'
              )}
              rows={3}
              {...field}
            />
          </FormControl>
          <FormDescription>
            {t('Default system prompt for this channel')}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  return systemPromptFields
}

export function ChannelSystemPromptOverrideFields() {
  const { form, sensitiveLocked, t } = useChannelMutateContext()
  const systemPromptOverrideFields = (
    <FormField
      control={form.control}
      name='system_prompt_override'
      render={({ field }) => (
        <FormItem className='flex items-center justify-between'>
          <div className='space-y-0.5'>
            <FormLabel>{t('System Prompt Concatenation')}</FormLabel>
            <FormDescription>
              {t('Concatenate channel system prompt with user&apos;s prompt')}
            </FormDescription>
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
  )

  return systemPromptOverrideFields
}

export function ChannelPassthroughFields() {
  const { form, sensitiveLocked, t } = useChannelMutateContext()
  const passthroughFields = (
    <FormField
      control={form.control}
      name='pass_through_body_enabled'
      render={({ field }) => (
        <FormItem className='flex items-center justify-between px-4 py-3'>
          <div className='space-y-0.5'>
            <FormLabel>{t('Pass Through Body')}</FormLabel>
            <FormDescription>
              {t('Pass request body directly to upstream')}
            </FormDescription>
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
  )

  return passthroughFields
}

export function ChannelThinkingFields() {
  const { form, sensitiveLocked, t } = useChannelMutateContext()
  const thinkingFields = (
    <FormField
      control={form.control}
      name='thinking_to_content'
      render={({ field }) => (
        <FormItem className='flex items-center justify-between px-4 py-3'>
          <div className='space-y-0.5'>
            <FormLabel>{t('Thinking to Content')}</FormLabel>
            <FormDescription>
              {t('Convert reasoning_content to <think> tag in content')}
            </FormDescription>
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
  )

  return thinkingFields
}

export function ChannelFormatFields() {
  const { currentType, form, sensitiveLocked, t } = useChannelMutateContext()
  const formatFields = currentType === 1 && (
    <FormField
      control={form.control}
      name='force_format'
      render={({ field }) => (
        <FormItem className='flex items-center justify-between px-4 py-3'>
          <div className='space-y-0.5'>
            <FormLabel>{t('Force Format')}</FormLabel>
            <FormDescription>
              {t(
                'Force format response to OpenAI standard (OpenAI channel only)'
              )}
            </FormDescription>
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
  )

  return formatFields
}
