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
import { Loader2, Copy, Eraser, Plus, RefreshCw, X } from 'lucide-react'

import { ErrorState } from '@/components/error-state'
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
import { Separator } from '@/components/ui/separator'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { FIELD_DESCRIPTIONS, MODEL_FETCHABLE_TYPES } from '../../constants'
import { useChannelMutateContext } from './channel-mutate-context'
import { formatModelNames } from './channel-mutate-dialog-model'
import { ChannelModelsSection } from './sections'

export function ChannelModelsFields() {
  const {
    canDiscoverModels,
    currentModelsArray,
    currentType,
    discovery,
    discoveryConnectionReady,
    discoveryMessage,
    form,
    handleAddManualModel,
    handleClearModels,
    handleCopyModels,
    handleFetchModels,
    handleRemoveModel,
    isBatchMode,
    isEditing,
    modelMappingGuardrail,
    newModelDraft,
    normalizedDiscoveredModels,
    previewModels,
    setNewModelDraft,
    t,
    updateModels,
  } = useChannelMutateContext()
  const modelsSection = (
    <div className='scroll-mt-4'>
      <ChannelModelsSection>
        <div className='space-y-5'>
          <div className='space-y-2'>
            <div className='space-y-2'>
              <h3 className='text-sm font-semibold'>
                {t('Fetch models from upstream')}
              </h3>
              {MODEL_FETCHABLE_TYPES.has(currentType) &&
                discovery.status === 'error' && (
                  <ErrorState
                    className='mt-4 min-h-0 p-3'
                    title={t('Failed to fetch models')}
                    description={getServerErrorMessage(
                      discovery.error,
                      t('Failed to fetch models')
                    )}
                    onRetry={() => {
                      void handleFetchModels()
                    }}
                  />
                )}
              {MODEL_FETCHABLE_TYPES.has(currentType) &&
                discovery.status !== 'error' && (
                  <div
                    role='status'
                    aria-live='polite'
                    className='border-border/60 bg-muted/20 space-y-3 rounded-lg border p-3'
                  >
                    <div className='flex flex-wrap items-center justify-between gap-2'>
                      <div className='flex min-w-0 items-center gap-2'>
                        {discovery.status === 'loading' && (
                          <Loader2
                            className='text-muted-foreground size-3.5 shrink-0 animate-spin'
                            aria-hidden='true'
                          />
                        )}
                        <span className='text-muted-foreground min-w-0 text-xs'>
                          {discoveryMessage}
                        </span>
                      </div>
                      {canDiscoverModels ? (
                        <div className='flex flex-wrap items-center gap-2'>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={handleFetchModels}
                            disabled={
                              discovery.status === 'loading' ||
                              !discoveryConnectionReady
                            }
                          >
                            <RefreshCw
                              className='mr-1.5 size-3.5'
                              aria-hidden='true'
                            />
                            {discovery.status === 'success' ||
                            discovery.status === 'stale'
                              ? t('Re-fetch')
                              : t('Probe upstream models')}
                          </Button>
                        </div>
                      ) : (
                        <span className='text-muted-foreground text-xs'>
                          {t('No permission to perform this action')}
                        </span>
                      )}
                    </div>
                    {discovery.status === 'success' &&
                      normalizedDiscoveredModels.length === 0 && (
                        <div className='space-y-1'>
                          <p className='text-xs font-medium'>
                            {t('No models returned by the upstream')}
                          </p>
                          <p className='text-muted-foreground text-xs'>
                            {t(
                              'You can add models manually or try fetching again.'
                            )}
                          </p>
                        </div>
                      )}
                    {isEditing && !previewModels && (
                      <p className='text-muted-foreground text-xs'>
                        {t(
                          'Model discovery uses the saved channel connection settings.'
                        )}
                      </p>
                    )}
                    {!isEditing && isBatchMode && (
                      <p className='text-muted-foreground text-xs'>
                        {t(
                          'Model discovery uses the first key; other keys are not tested.'
                        )}
                      </p>
                    )}
                  </div>
                )}
            </div>

            <FormField
              control={form.control}
              name='models'
              render={() => (
                <FormItem
                  role='group'
                  aria-label={t('Models')}
                  className='space-y-3'
                >
                  <div className='flex flex-wrap items-start justify-between gap-2'>
                    <div className='min-w-0 space-y-1'>
                      <FormLabel required>{t('Models')}</FormLabel>
                      <FormDescription>
                        {t(FIELD_DESCRIPTIONS.MODELS)}
                      </FormDescription>
                    </div>
                  </div>
                  <div className='space-y-2'>
                    <span className='text-sm font-medium'>
                      {t('Selected models')}
                    </span>
                    {currentModelsArray.length > 0 ? (
                      <ul className='divide-border/60 divide-y rounded-lg border'>
                        {currentModelsArray.map((model) => (
                          <li
                            key={model}
                            className='flex items-center justify-between gap-2 px-3 py-1.5'
                          >
                            <span className='min-w-0 text-sm wrap-anywhere'>
                              {model}
                            </span>
                            <Button
                              type='button'
                              variant='ghost'
                              size='icon-sm'
                              aria-label={t('Remove {{model}}', { model })}
                              onClick={() => handleRemoveModel(model)}
                            >
                              <X className='size-4' aria-hidden='true' />
                            </Button>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className='text-muted-foreground text-sm'>
                        {t('No models selected')}
                      </p>
                    )}
                  </div>
                  <div className='flex items-center gap-2'>
                    <FormControl>
                      <Input
                        aria-label={t('Add a model manually')}
                        placeholder={t(
                          'Model name (separate multiple with commas or newlines)'
                        )}
                        value={newModelDraft}
                        onChange={(event) =>
                          setNewModelDraft(event.target.value)
                        }
                        onKeyDown={(event) => {
                          if (event.key !== 'Enter') return
                          event.preventDefault()
                          handleAddManualModel()
                        }}
                        onPaste={(event) => {
                          // 单行 input 会丢掉换行：先取剪贴板原文，含分隔符就整批添加。
                          const text =
                            event.clipboardData?.getData('text') ?? ''
                          if (!text.trim() || !/[\s,，、]/.test(text.trim())) {
                            return
                          }
                          event.preventDefault()
                          handleAddManualModel(text)
                        }}
                      />
                    </FormControl>
                    <Button
                      type='button'
                      variant='outline'
                      className='shrink-0'
                      disabled={!newModelDraft.trim()}
                      onClick={() => handleAddManualModel()}
                    >
                      <Plus className='mr-2 h-4 w-4' aria-hidden='true' />
                      {t('Add')}
                    </Button>
                  </div>
                  {modelMappingGuardrail.exposedTargetModels.length > 0 && (
                    <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
                      <AlertDescription className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                        <span>
                          {t('The mapped upstream model(s)')}{' '}
                          {formatModelNames(
                            modelMappingGuardrail.exposedTargetModels
                          )}{' '}
                          {t(
                            'are also listed here. Remove them from Models to keep the `/v1/models` response user-friendly and hide vendor-specific names.'
                          )}
                        </span>
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          onClick={() => {
                            const hiddenTargets = new Set(
                              modelMappingGuardrail.exposedTargetModels
                            )
                            updateModels(
                              currentModelsArray.filter(
                                (model) => !hiddenTargets.has(model)
                              )
                            )
                          }}
                        >
                          {t('Remove mapped targets')}
                        </Button>
                      </AlertDescription>
                    </Alert>
                  )}
                  <FormMessage />
                </FormItem>
              )}
            />

            <Separator className='my-4' />

            <div className='flex flex-wrap items-center gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={handleCopyModels}
                disabled={currentModelsArray.length === 0}
              >
                <Copy className='mr-2 h-4 w-4' aria-hidden='true' />
                {t('Copy All')}
              </Button>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={handleClearModels}
                disabled={currentModelsArray.length === 0}
              >
                <Eraser className='mr-2 h-4 w-4' aria-hidden='true' />
                {t('Clear All')}
              </Button>
            </div>
          </div>
        </div>
      </ChannelModelsSection>
    </div>
  )

  return modelsSection
}
