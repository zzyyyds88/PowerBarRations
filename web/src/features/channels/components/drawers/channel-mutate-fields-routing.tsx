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
import { ArrowRight, Route } from 'lucide-react'

import { LearnMore } from '@/components/learn-more'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { PopoverDescription, PopoverTitle } from '@/components/ui/popover'

import { FIELD_DESCRIPTIONS, FIELD_PLACEHOLDERS } from '../../constants'
import { ModelMappingEditor } from '../model-mapping-editor'
import { ChannelConfigurationStatusIndicator } from './channel-configuration'
import { useChannelMutateContext } from './channel-mutate-context'
import {
  channelConfigurationBlockClassName,
  formatModelNames,
} from './channel-mutate-dialog-model'
import { SubHeading } from './channel-mutate-dialog-parts'

export function ChannelRoutingFields() {
  const { configuration, form, t } = useChannelMutateContext()
  const routingFields = (
    <div
      role='group'
      aria-label={t('Routing Strategy')}
      className={channelConfigurationBlockClassName(
        configuration.blocks.routingStrategy,
        'flex flex-col gap-4'
      )}
    >
      <SubHeading
        title={t('Routing Strategy')}
        status={configuration.blocks.routingStrategy}
        icon={<Route className='h-3.5 w-3.5' />}
        iconTone='info'
      />
      <FormField
        control={form.control}
        name='test_model'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Test Model')}</FormLabel>
            <FormControl>
              <Input
                placeholder={t(FIELD_PLACEHOLDERS.TEST_MODEL)}
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t(FIELD_DESCRIPTIONS.TEST_MODEL)}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  )

  return routingFields
}

export function ChannelModelMappingFields() {
  const {
    configuration,
    currentModelsArray,
    form,
    isSubmitting,
    mappingPreviewPairs,
    mappingTargetOptions,
    modelMappingGuardrail,
    remainingMappingCount,
    t,
    updateModels,
  } = useChannelMutateContext()
  const modelMappingFields = (
    <div
      role='group'
      aria-label={t('Model Mapping')}
      className={channelConfigurationBlockClassName(
        configuration.blocks.modelMapping
      )}
    >
      <FormField
        control={form.control}
        name='model_mapping'
        render={({ field }) => (
          <FormItem className='space-y-3'>
            <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
              <div className='space-y-1'>
                <div className='flex items-center gap-2'>
                  <FormLabel className='mb-0'>{t('Model Mapping')}</FormLabel>
                  <ChannelConfigurationStatusIndicator
                    status={configuration.blocks.modelMapping}
                  />
                  <LearnMore
                    triggerProps={{
                      type: 'button',
                      'aria-label': t('How model mapping works'),
                      className: 'size-4 rounded-full',
                    }}
                    contentProps={{
                      collisionPadding: 16,
                      className:
                        'w-96 max-w-[calc(100vw-2rem)] gap-3 p-4 text-sm leading-6',
                    }}
                  >
                    <PopoverTitle className='text-foreground'>
                      {t('Request flow')}
                    </PopoverTitle>
                    <div className='space-y-2 font-mono'>
                      {mappingPreviewPairs.map((pair) => (
                        <div
                          key={`${pair.source}-${pair.target}`}
                          className='grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-start gap-3'
                        >
                          <span className='min-w-0 wrap-anywhere'>
                            {pair.source}
                          </span>
                          <ArrowRight
                            className='mt-1 size-4 shrink-0'
                            aria-hidden='true'
                          />
                          <span className='min-w-0 wrap-anywhere'>
                            {pair.target}
                          </span>
                        </div>
                      ))}
                      {remainingMappingCount > 0 && (
                        <div>
                          +{remainingMappingCount} {t('more mapping')}
                          {remainingMappingCount > 1 ? 's' : ''}
                        </div>
                      )}
                    </div>
                    <PopoverDescription className='leading-6'>
                      {t(
                        'Lane members pick the model on the left; this channel sends the upstream model on the right. One edit here applies to every lane.'
                      )}
                    </PopoverDescription>
                  </LearnMore>
                </div>
                <FormDescription>
                  {t(FIELD_DESCRIPTIONS.MODEL_MAPPING)}
                </FormDescription>
              </div>
            </div>
            <FormControl>
              <ModelMappingEditor
                value={field.value || ''}
                onChange={field.onChange}
                disabled={isSubmitting}
                sourceModelOptions={currentModelsArray}
                targetModelOptions={mappingTargetOptions}
              />
            </FormControl>
            {modelMappingGuardrail.invalidJson && (
              <Alert variant='destructive'>
                <AlertDescription>
                  {t('Model Mapping must be a JSON object like')}{' '}
                  <code className='font-mono'>{'{"gpt-4":"Azure-GPT4"}'}</code>
                  {t('. Please fix the JSON before saving.')}
                </AlertDescription>
              </Alert>
            )}
            {modelMappingGuardrail.missingSourceModels.length > 0 && (
              <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
                <AlertDescription className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                  <span>
                    {t('Add')}{' '}
                    {formatModelNames(
                      modelMappingGuardrail.missingSourceModels
                    )}{' '}
                    {t(
                      'to the Models list so users can use them before the mapping sends traffic upstream.'
                    )}
                  </span>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => {
                      updateModels([
                        ...currentModelsArray,
                        ...modelMappingGuardrail.missingSourceModels,
                      ])
                    }}
                  >
                    {t('Add missing models')}
                  </Button>
                </AlertDescription>
              </Alert>
            )}
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  )

  return modelMappingFields
}
