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
import { sideDrawerSwitchItemClassName } from '@/components/drawer-layout'
import { Combobox } from '@/components/ui/combobox'
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

import { FIELD_PLACEHOLDERS } from '../../constants'
import { PROVIDER_PRESETS } from '../../lib/provider-presets'
import { isChannelProtocol } from '../../types'
import { useChannelMutateContext } from './channel-mutate-context'
import { ChannelBasicSection } from './sections'

export function ChannelBasicSectionFields() {
  const {
    applyProviderPreset,
    channelProtocolComboboxOptions,
    currentProtocol,
    form,
    isEditing,
    isSubmitting,
    providerPresetOptions,
    selectChannelProtocol,
    sensitiveLocked,
    t,
  } = useChannelMutateContext()
  const basicSection = (
    <div className='scroll-mt-4'>
      <ChannelBasicSection>
        {/* 新建渠道：先选服务商，协议/地址/名称自动填好，只剩 key 要填。 */}
        {!isEditing && (
          <div className='border-border/60 bg-muted/20 gap-2 rounded-lg border p-3'>
            <div className='flex flex-wrap items-center gap-2'>
              <FormLabel className='mb-0'>
                {t('Quick setup: pick a provider')}
              </FormLabel>
              <span className='text-muted-foreground text-xs'>
                {t(
                  'Fills protocol, base URL and name. Only the API key is left.'
                )}
              </span>
            </div>
            <Combobox
              options={providerPresetOptions}
              value=''
              onValueChange={(value) => {
                const preset = PROVIDER_PRESETS.find(
                  (item) => item.id === value
                )
                if (preset) applyProviderPreset(preset)
              }}
              disabled={sensitiveLocked || isSubmitting}
              placeholder={t('Choose a provider (optional)')}
              searchPlaceholder={t('Search...')}
              aria-label={t('Provider presets')}
            />
          </div>
        )}
        <div className='grid gap-4 sm:grid-cols-2'>
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel required>{t('Name')}</FormLabel>
                <FormControl>
                  <Input placeholder={t(FIELD_PLACEHOLDERS.NAME)} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='type'
            render={({ field }) => {
              // 协议已确定 → 显示协议；旧厂商类型 → 显示"当前值"选项。
              const comboboxValue =
                currentProtocol !== ''
                  ? `protocol:${currentProtocol}`
                  : `type:${field.value ?? ''}`
              return (
                <FormItem>
                  <FormLabel required>{t('Protocol')}</FormLabel>
                  <FormControl>
                    <Combobox
                      options={channelProtocolComboboxOptions}
                      value={comboboxValue}
                      onValueChange={(value) => {
                        if (!value) return
                        if (value.startsWith('protocol:')) {
                          const parsed = value.slice('protocol:'.length)
                          if (isChannelProtocol(parsed)) {
                            selectChannelProtocol(parsed)
                          }
                        }
                        // `type:<n>` 是旧厂商类型的"当前值"选项：选中即保持原类型不变。
                      }}
                      disabled={sensitiveLocked || isSubmitting}
                      placeholder={t('Protocol')}
                      searchPlaceholder={t('Search...')}
                      aria-label={t('Protocol')}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )
            }}
          />
        </div>

        {!isEditing && (
          <FormField
            control={form.control}
            name='status'
            render={({ field }) => (
              <FormItem className={sideDrawerSwitchItemClassName()}>
                <div className='flex flex-col gap-0.5'>
                  <FormLabel>{t('Enabled')}</FormLabel>
                  <FormDescription className='text-xs'>
                    {t('Enable or disable this channel')}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value === 1}
                    onCheckedChange={(checked) =>
                      field.onChange(checked ? 1 : 2)
                    }
                  />
                </FormControl>
              </FormItem>
            )}
          />
        )}
      </ChannelBasicSection>
    </div>
  )

  return basicSection
}
