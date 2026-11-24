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
import { ClipboardPaste, Loader2, Server, Settings } from 'lucide-react'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog as DialogRoot,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Form } from '@/components/ui/form'
import { IconBadge } from '@/components/ui/icon-badge'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { CHANNEL_STATUS_LABELS } from '../../constants'
import { CHANNEL_FORM_DEFAULT_VALUES } from '../../lib'
import { AdvancedCustomEditorDialog } from '../dialogs/advanced-custom-editor-dialog'
import { ConfigureModelsDialog } from '../dialogs/configure-models-dialog'
import { MissingModelsConfirmationDialog } from '../dialogs/missing-models-confirmation-dialog'
import { ParamOverrideEditorDialog } from '../dialogs/param-override-editor-dialog'
import { StatusCodeRiskDialog } from '../dialogs/status-code-risk-dialog'
import { ChannelConfiguration } from './channel-configuration'
import { useChannelMutateContext } from './channel-mutate-context'
import { channelConfigurationBlockClassName } from './channel-mutate-dialog-model'
import { CardHeading } from './channel-mutate-dialog-parts'
import {
  ChannelNotesFields,
  ChannelHttpProtocolFields,
  ChannelHttpShardsFields,
  ChannelUpstreamModelDetectionFields,
} from './channel-mutate-fields-other'
import {
  ChannelFieldPassthroughFields,
  ChannelOverrideFields,
} from './channel-mutate-fields-override'
import {
  ChannelFormatFields,
  ChannelPassthroughFields,
  ChannelPricesFields,
  ChannelRateLimitsFields,
  ChannelProxyFields,
  ChannelSystemPromptFields,
  ChannelSystemPromptOverrideFields,
  ChannelThinkingFields,
} from './channel-mutate-fields-request'
import {
  ChannelModelMappingFields,
  ChannelRoutingFields,
} from './channel-mutate-fields-routing'
import { ChannelBasicSectionFields } from './channel-mutate-section-basic'
import { ChannelConnectionFields } from './channel-mutate-section-connection'
import { ChannelModelsFields } from './channel-mutate-section-models'
import { ChannelEditorLoadingState } from './sections'

export function ChannelMutateDialogView(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const {
    t,
    form,
    formValues,
    initialModels,
    isEditing,
    isChannelError,
    channelData,
    channelError,
    refetchChannel,
    isChannelDetailLoading,
    configurationSection,
    setConfigurationSection,
    configuration,
    sensitiveLocked,
    handleOpenChange,
    isSubmitting,
    canEditSensitive,
    onSubmit,
    onInvalid,
    currentStatus,
    channelFormRef,
    clipboardConnectionInfo,
    setClipboardConnectionInfo,
    applyConnectionInfo,
    pasteConnectionInfoFromClipboard,
    modelDiscoveryDialogOpen,
    setModelDiscoveryDialogOpen,
    normalizedDiscoveredModels,
    currentModelsArray,
    redirectModelList,
    redirectModelKeyList,
    handleModelDiscoveryApply,
    paramOverrideEditorOpen,
    setParamOverrideEditorOpen,
    advancedCustomEditorOpen,
    setAdvancedCustomEditorOpen,
    missingModelsDialogOpen,
    setMissingModelsDialogOpen,
    missingModelsList,
    handleMissingModelsAction,
    statusCodeRiskOpen,
    statusCodeRiskDetailItems,
    handleStatusCodeRiskAction,
    referencedModelsDialogOpen,
    referencedModelsLanes,
    handleReferencedModelsAction,
    clearModelsConfirmOpen,
    setClearModelsConfirmOpen,
    handleConfirmClearModels,
    discardOpen,
    setDiscardOpen,
  } = useChannelMutateContext()

  let formContent
  if (isEditing && isChannelError && !channelData) {
    formContent = (
      <ErrorState
        title={t('Failed to load channel')}
        description={getServerErrorMessage(
          channelError,
          t('Failed to load channel')
        )}
        onRetry={() => {
          void refetchChannel()
        }}
      />
    )
  } else if (isChannelDetailLoading) {
    formContent = <ChannelEditorLoadingState />
  } else {
    formContent = (
      <ChannelConfiguration
        section={configurationSection}
        onSectionChange={setConfigurationSection}
        statuses={configuration.sections}
        connection={
          <>
            <ChannelBasicSectionFields />
            <ChannelConnectionFields />
          </>
        }
        models={<ChannelModelsFields />}
        routing={
          <>
            <ChannelModelMappingFields />
            <ChannelRoutingFields />
          </>
        }
        prices={
          <div className='border-border/60 bg-muted/10 rounded-lg border p-4'>
            <ChannelPricesFields />
          </div>
        }
        rateLimits={
          <div className='border-border/60 bg-muted/10 rounded-lg border p-4'>
            <ChannelRateLimitsFields />
          </div>
        }
        request={
          <>
            <div
              role='group'
              aria-label={t('Request processing')}
              className={channelConfigurationBlockClassName(
                configuration.blocks.requestProcessing,
                'space-y-4'
              )}
            >
              <CardHeading
                status={configuration.blocks.requestProcessing}
                title={t('Request processing')}
                icon={<Settings className='size-4' />}
              />
              <fieldset
                disabled={sensitiveLocked}
                className='space-y-4 disabled:opacity-60'
              >
                <ChannelFormatFields />
                <ChannelThinkingFields />
                <ChannelPassthroughFields />
                <ChannelSystemPromptFields />
                <ChannelSystemPromptOverrideFields />
              </fieldset>
            </div>
            <ChannelOverrideFields />
            <ChannelFieldPassthroughFields />
          </>
        }
        other={
          <>
            <div
              role='group'
              aria-label={t('Channel Extra Settings')}
              className={channelConfigurationBlockClassName(
                configuration.blocks.extraSettings,
                'space-y-4'
              )}
            >
              <CardHeading
                status={configuration.blocks.extraSettings}
                title={t('Channel Extra Settings')}
                icon={<Settings className='size-4' />}
              />
              <fieldset
                disabled={sensitiveLocked}
                className='space-y-4 disabled:opacity-60'
              >
                <ChannelProxyFields />
                <ChannelHttpProtocolFields />
                <ChannelHttpShardsFields />
              </fieldset>
            </div>
            <ChannelUpstreamModelDetectionFields />
            <ChannelNotesFields />
          </>
        }
      />
    )
  }

  let description = t(
    'Configure the connection and models, then create the channel.'
  )
  if (isEditing) {
    description = t(
      "Update channel configuration and click save when you're done."
    )
  }

  return (
    <>
      <DialogRoot open={props.open} onOpenChange={handleOpenChange}>
        <DialogContent
          variant='sheet'
          className='flex w-full flex-col gap-4 overflow-hidden p-4 sm:w-[min(98vw,1040px)] sm:max-w-none sm:p-6'
        >
          <DialogHeader className='pr-12'>
            <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
              <div className='min-w-0 flex-1'>
                <div className='flex min-w-0 items-center gap-2 sm:gap-3'>
                  <DialogTitle className='flex shrink-0 items-center gap-2 sm:gap-3'>
                    <IconBadge tone='info' size='title'>
                      <Server className='size-5' />
                    </IconBadge>
                    <span>
                      {isEditing ? t('Edit Channel') : t('Create Channel')}
                    </span>
                  </DialogTitle>
                </div>
                {isEditing && channelData && (
                  <Badge variant='secondary' className='mt-2'>
                    {t(
                      CHANNEL_STATUS_LABELS[
                        currentStatus as keyof typeof CHANNEL_STATUS_LABELS
                      ] || 'Unknown'
                    )}
                  </Badge>
                )}
                <DialogDescription className='mt-1'>
                  {description}
                </DialogDescription>
              </div>
              {!isEditing && (
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  className='shrink-0'
                  onClick={pasteConnectionInfoFromClipboard}
                >
                  <ClipboardPaste className='size-4' />
                  <span>{t('Paste Connection Info')}</span>
                </Button>
              )}
            </div>
          </DialogHeader>

          {sensitiveLocked && (
            <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
              <AlertDescription>
                {t(
                  'Sensitive channel settings are read-only for your account.'
                )}{' '}
                {t(
                  'You can still edit non-sensitive operations fields such as models and groups.'
                )}
              </AlertDescription>
            </Alert>
          )}

          {!isEditing && clipboardConnectionInfo && (
            <Alert>
              <AlertDescription className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                <span>{t('Connection info detected in clipboard')}</span>
                <span className='flex shrink-0 gap-2'>
                  <Button
                    type='button'
                    size='sm'
                    onClick={() => applyConnectionInfo(clipboardConnectionInfo)}
                  >
                    {t('Fill in')}
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    onClick={() => setClipboardConnectionInfo(null)}
                  >
                    {t('Ignore')}
                  </Button>
                </span>
              </AlertDescription>
            </Alert>
          )}

          <Form {...form}>
            <form
              id='channel-form'
              ref={channelFormRef}
              onSubmit={form.handleSubmit(onSubmit, onInvalid)}
              className='flex min-h-0 flex-1 flex-col gap-5 overflow-hidden'
            >
              {formContent}
            </form>
          </Form>

          <DialogFooter className='mt-2'>
            <Button
              type='button'
              variant='outline'
              disabled={isSubmitting}
              onClick={() => handleOpenChange(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              form='channel-form'
              type='submit'
              disabled={
                isSubmitting ||
                (!isEditing && !canEditSensitive) ||
                (isEditing && !channelData)
              }
            >
              {isSubmitting && (
                <Loader2 className='mr-2 h-4 w-4 animate-spin' />
              )}
              {isEditing ? t('Update Channel') : t('Create Channel')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </DialogRoot>

      {modelDiscoveryDialogOpen && (
        <ConfigureModelsDialog
          open
          candidates={normalizedDiscoveredModels}
          selectedModels={currentModelsArray}
          existingModels={isEditing ? initialModels : currentModelsArray}
          redirectModels={redirectModelList}
          redirectSourceModels={redirectModelKeyList}
          onOpenChange={setModelDiscoveryDialogOpen}
          onApply={handleModelDiscoveryApply}
        />
      )}

      {paramOverrideEditorOpen && !sensitiveLocked && (
        <ParamOverrideEditorDialog
          open={paramOverrideEditorOpen}
          value={formValues.param_override || ''}
          onOpenChange={setParamOverrideEditorOpen}
          onSave={(nextValue) => {
            form.setValue('param_override', nextValue, {
              shouldDirty: true,
              shouldValidate: true,
            })
          }}
        />
      )}

      {advancedCustomEditorOpen && !sensitiveLocked && (
        <AdvancedCustomEditorDialog
          open={advancedCustomEditorOpen}
          value={formValues.advanced_custom || ''}
          onOpenChange={setAdvancedCustomEditorOpen}
          onSave={(nextValue) => {
            form.setValue('advanced_custom', nextValue, {
              shouldDirty: true,
              shouldValidate: true,
            })
          }}
        />
      )}

      <MissingModelsConfirmationDialog
        open={missingModelsDialogOpen}
        missingModels={missingModelsList}
        onConfirm={handleMissingModelsAction}
        onOpenChange={setMissingModelsDialogOpen}
      />

      <StatusCodeRiskDialog
        open={statusCodeRiskOpen}
        onOpenChange={(v) => {
          if (!v) handleStatusCodeRiskAction(false)
        }}
        detailItems={statusCodeRiskDetailItems}
        onConfirm={() => handleStatusCodeRiskAction(true)}
      />

      <ConfirmDialog
        open={referencedModelsDialogOpen}
        onOpenChange={(v) => {
          if (!v) handleReferencedModelsAction(false)
        }}
        title={t('Models still referenced by lanes')}
        desc={
          <div className='space-y-2'>
            <p>
              {t('These models are still referenced by the following lanes:')}
            </p>
            <ul className='list-disc space-y-1 ps-5'>
              {referencedModelsLanes.map((lane) => (
                <li key={lane}>
                  <a
                    href={`/routes?model=${encodeURIComponent(lane)}`}
                    className='underline underline-offset-2'
                  >
                    {lane}
                  </a>
                </li>
              ))}
            </ul>
            <p>
              {t(
                'If you continue, this channel will be removed from those lanes and empty lanes will be deleted. Those models become uncallable until a new lane member is configured.'
              )}
            </p>
          </div>
        }
        confirmText={t('Remove and save')}
        destructive
        handleConfirm={() => handleReferencedModelsAction(true)}
      />

      <ConfirmDialog
        open={clearModelsConfirmOpen}
        onOpenChange={setClearModelsConfirmOpen}
        title={t('Clear all models?')}
        desc={t(
          'This only clears the draft model list. The channel is not updated until you save.'
        )}
        confirmText={t('Clear draft')}
        handleConfirm={handleConfirmClearModels}
      />

      <ConfirmDialog
        destructive
        open={discardOpen}
        onOpenChange={setDiscardOpen}
        title={t('Discard unsaved changes?')}
        desc={t(
          'This channel has unsaved changes. Closing now will discard them.'
        )}
        confirmText={t('Discard changes')}
        handleConfirm={() => {
          setDiscardOpen(false)
          form.reset(CHANNEL_FORM_DEFAULT_VALUES)
          setClipboardConnectionInfo(null)
          props.onOpenChange(false)
        }}
      />
    </>
  )
}
