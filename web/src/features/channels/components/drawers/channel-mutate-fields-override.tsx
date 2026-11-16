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
import { Code, SlidersHorizontal, Wand2 } from 'lucide-react'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { JsonEditor } from '@/components/json-editor'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'

import {
  CLAUDE_FIELD_PASSTHROUGH_TYPES,
  FIELD_PASSTHROUGH_TYPES,
  OPENAI_FIELD_PASSTHROUGH_TYPES,
} from '../../constants'
import { useChannelMutateContext } from './channel-mutate-context'
import { channelConfigurationBlockClassName } from './channel-mutate-dialog-model'
import { CardHeading, SubHeading } from './channel-mutate-dialog-parts'

export function ChannelOverrideFields() {
  const {
    configuration,
    form,
    isSubmitting,
    sensitiveLocked,
    setParamOverrideEditorOpen,
    t,
  } = useChannelMutateContext()
  const overrideFields = (
    <div
      role='group'
      aria-label={t('Override Rules')}
      className={channelConfigurationBlockClassName(
        configuration.blocks.overrideRules,
        'flex flex-col gap-4'
      )}
    >
      <SubHeading
        title={t('Override Rules')}
        status={configuration.blocks.overrideRules}
        icon={<Code className='h-3.5 w-3.5' />}
        iconTone='chart-4'
      />

      <FormField
        control={form.control}
        name='status_code_mapping'
        render={({ field }) => (
          <FormItem className='space-y-3'>
            <div className='space-y-1'>
              <FormLabel>{t('Status Code Mapping')}</FormLabel>
              <FormDescription>
                {t('Map upstream status codes to different codes')}
              </FormDescription>
            </div>
            <FormControl>
              <JsonEditor
                value={field.value || ''}
                onChange={field.onChange}
                disabled={isSubmitting}
                keyPlaceholder='400'
                valuePlaceholder='500'
                keyLabel='Original Code'
                valueLabel='Mapped Code'
                emptyMessage={t('No status code mappings configured.')}
                template={{ '400': '500', '429': '503' }}
                valueType='string'
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      {sensitiveLocked && (
        <p className='text-muted-foreground text-xs'>
          {t('No permission to perform this action')}
        </p>
      )}
      <fieldset
        disabled={sensitiveLocked}
        className='space-y-4 disabled:opacity-60'
      >
        <FormField
          control={form.control}
          name='param_override'
          render={({ field }) => (
            <FormItem className='space-y-3 border-t pt-4'>
              <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
                <div className='space-y-1'>
                  <FormLabel>{t('Parameter Override')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Override request parameters. Cannot override stream parameter.'
                    )}
                  </FormDescription>
                </div>
                <div className='flex flex-wrap gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => setParamOverrideEditorOpen(true)}
                  >
                    <Wand2 className='mr-2 h-4 w-4' />
                    {t('Visual edit')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => {
                      field.onChange(
                        JSON.stringify(
                          {
                            operations: [
                              {
                                path: 'temperature',
                                mode: 'set',
                                value: 0.7,
                                conditions: [
                                  {
                                    path: 'model',
                                    mode: 'prefix',
                                    value: 'gpt',
                                  },
                                ],
                                logic: 'AND',
                              },
                            ],
                          },
                          null,
                          2
                        )
                      )
                    }}
                  >
                    <Code className='mr-2 h-4 w-4' />
                    {t('New Format Template')}
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    onClick={() => field.onChange('')}
                  >
                    {t('Clear')}
                  </Button>
                </div>
              </div>
              <FormControl>
                <JsonCodeEditor
                  value={field.value || ''}
                  onChange={field.onChange}
                  name={field.name}
                  onBlur={field.onBlur}
                  textareaRef={field.ref}
                  disabled={sensitiveLocked || isSubmitting}
                  placeholder={t(
                    'Override request parameters. Cannot override stream parameter.'
                  )}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='header_override'
          render={({ field }) => (
            <FormItem className='space-y-3 border-t pt-4'>
              <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
                <div className='space-y-1'>
                  <FormLabel>{t('Request Header Override')}</FormLabel>
                  <FormDescription>
                    {t('Override request headers')}
                  </FormDescription>
                </div>
                <div className='flex flex-wrap gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      field.onChange(
                        JSON.stringify(
                          {
                            '*': true,
                            're:^X-Trace-.*$': true,
                            'X-Foo': '{client_header:X-Foo}',
                            Authorization: 'Bearer {api_key}',
                          },
                          null,
                          2
                        )
                      )
                    }
                  >
                    {t('Fill Template')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      field.onChange(JSON.stringify({ '*': true }, null, 2))
                    }
                  >
                    {t('Passthrough Template')}
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    onClick={() => field.onChange('')}
                  >
                    {t('Clear')}
                  </Button>
                </div>
              </div>
              <FormControl>
                <JsonCodeEditor
                  value={field.value || ''}
                  onChange={field.onChange}
                  name={field.name}
                  onBlur={field.onBlur}
                  textareaRef={field.ref}
                  disabled={sensitiveLocked || isSubmitting}
                  placeholder={t('Enter JSON to override request headers')}
                  heightClassName='h-40 min-h-40 max-h-40'
                />
              </FormControl>
              <FormDescription className='text-xs'>
                {t('Supported variables')}:{' '}
                <code className='bg-muted rounded px-1 py-0.5'>
                  {'{api_key}'}
                </code>{' '}
                — {t('Channel key')},{' '}
                <code className='bg-muted rounded px-1 py-0.5'>
                  {'{client_header:NAME}'}
                </code>{' '}
                — {t('Client header value')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </fieldset>
    </div>
  )

  return overrideFields
}

export function ChannelFieldPassthroughFields() {
  const { configuration, currentType, form, sensitiveLocked, t } =
    useChannelMutateContext()
  const fieldPassthroughFields = FIELD_PASSTHROUGH_TYPES.has(currentType) && (
    <div
      role='group'
      aria-label={t('Field passthrough controls')}
      className={channelConfigurationBlockClassName(
        configuration.blocks.fieldPassthrough,
        'flex flex-col gap-4'
      )}
    >
      <CardHeading
        title={t('Field passthrough controls')}
        status={configuration.blocks.fieldPassthrough}
        icon={<SlidersHorizontal className='h-4 w-4' />}
        iconTone='chart-4'
      />
      <fieldset disabled={sensitiveLocked} className='disabled:opacity-60'>
        <div className='border-border grid grid-cols-1 gap-y-2 border-y sm:grid-cols-2'>
          <FormField
            control={form.control}
            name='allow_service_tier'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-sm'>
                    {t('Allow service_tier passthrough')}
                  </FormLabel>
                  <FormDescription>
                    {t('Pass through the service_tier field')}
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

          {OPENAI_FIELD_PASSTHROUGH_TYPES.has(currentType) && (
            <>
              <FormField
                control={form.control}
                name='disable_store'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                    <div className='space-y-0.5'>
                      <FormLabel className='text-sm'>
                        {t('Disable store passthrough')}
                      </FormLabel>
                      <FormDescription>
                        {t('When enabled, the store field will be blocked')}
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

              <FormField
                control={form.control}
                name='allow_safety_identifier'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                    <div className='space-y-0.5'>
                      <FormLabel className='text-sm'>
                        {t('Allow safety_identifier passthrough')}
                      </FormLabel>
                      <FormDescription>
                        {t('Pass through the safety_identifier field')}
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

              <FormField
                control={form.control}
                name='allow_include_obfuscation'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                    <div className='space-y-0.5'>
                      <FormLabel className='text-sm'>
                        {t('Allow include usage obfuscation passthrough')}
                      </FormLabel>
                      <FormDescription>
                        {t(
                          'Pass through the include field for usage obfuscation'
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

              <FormField
                control={form.control}
                name='allow_inference_geo'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                    <div className='space-y-0.5'>
                      <FormLabel className='text-sm'>
                        {t('Allow inference geography passthrough')}
                      </FormLabel>
                      <FormDescription>
                        {t(
                          'Pass through the inference_geo field for geographic routing'
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
            </>
          )}

          {CLAUDE_FIELD_PASSTHROUGH_TYPES.has(currentType) && (
            <>
              {currentType === 14 && (
                <FormField
                  control={form.control}
                  name='allow_inference_geo'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                      <div className='space-y-0.5'>
                        <FormLabel className='text-sm'>
                          {t('Allow inference_geo passthrough')}
                        </FormLabel>
                        <FormDescription>
                          {t(
                            'Pass through the inference_geo field for Claude data residency region control'
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
              )}

              <FormField
                control={form.control}
                name='allow_speed'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                    <div className='space-y-0.5'>
                      <FormLabel className='text-sm'>
                        {t('Allow speed passthrough')}
                      </FormLabel>
                      <FormDescription>
                        {t(
                          'Pass through the speed field for Claude inference speed mode control'
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

              {currentType === 14 && (
                <FormField
                  control={form.control}
                  name='claude_beta_query'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
                      <div className='space-y-0.5'>
                        <FormLabel className='text-sm'>
                          {t('Allow Claude beta query passthrough')}
                        </FormLabel>
                        <FormDescription>
                          {t(
                            'Pass through the anthropic-beta header for beta features'
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
              )}
            </>
          )}
        </div>
      </fieldset>
    </div>
  )

  return fieldPassthroughFields
}
