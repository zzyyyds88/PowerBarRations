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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { SideDrawerSection } from '@/components/drawer-layout'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { LobeIconField } from '@/components/lobe-icon-field'
import { TagInput } from '@/components/tag-input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { resolveModelProvider } from '@/lib/model-provider'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { createModel, updateModel, getModel } from '../../api'
import { modelsQueryKeys } from '../../lib'
import {
  modelFormSchema,
  transformModelToFormDefaults,
  transformFormDataToModelPayload,
  type ModelFormValues,
} from '../../lib/model-form'
import type { Model } from '../../types'
import { ModelLinkedChannels } from '../model-linked-channels'

// 编辑模型走居中弹窗（ui-spec §6.3 / §6.9）：分区只保留「基本信息」与「渠道关联」，
// 与「编辑渠道」同一规范。Escape/Cancel 关闭并丢弃未保存草稿（有改动时先确认）。

export function ModelMutateDrawer(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: Model | null
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [createdModel, setCreatedModel] = useState<{
    source: Model | null | undefined
    model: Model
  } | null>(null)
  const currentRow =
    createdModel && createdModel.source === props.currentRow
      ? createdModel.model
      : props.currentRow
  const isEditing = Boolean(currentRow?.id)
  const hasModelName = Boolean(currentRow?.model_name)
  const [closeConfirm, setCloseConfirm] = useState(false)
  const loadedKey = useRef('')
  const form = useForm({
    resolver: zodResolver(modelFormSchema),
    defaultValues: transformModelToFormDefaults({
      model_name: '',
      status: 1,
      sync_official: 1,
      name_rule: 0,
    } as Model),
  })
  // 用 useWatch 而非 form.watch：后者与 React Compiler 不兼容（lint 规则）。
  const watchedModelName = useWatch({
    control: form.control,
    name: 'model_name',
  })
  const modelQuery = useQuery({
    queryKey: modelsQueryKeys.detail(currentRow?.id ?? 0),
    // 成功即裸模型对象；失败由 axios 拒绝。
    queryFn: async () => {
      if (!currentRow?.id) throw new Error(t('Model ID is required'))
      return getModel(currentRow.id)
    },
    enabled: props.open && isEditing,
  })
  useEffect(() => {
    if (!props.open) return
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setCloseConfirm(false)
  }, [props.open, props.currentRow?.id, props.currentRow?.model_name])

  useEffect(() => {
    if (!props.open) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setCreatedModel(null)
      loadedKey.current = ''
      return
    }
    const key = currentRow?.id
      ? `metadata:${currentRow.id}`
      : `channel:${currentRow?.model_name ?? ''}`
    if (loadedKey.current === key || (isEditing && !modelQuery.data)) return
    form.reset(
      transformModelToFormDefaults(
        (isEditing
          ? modelQuery.data
          : {
              model_name: currentRow?.model_name ?? '',
              status: 1,
              sync_official: 1,
              name_rule: 0,
            }) as Model
      )
    )
    loadedKey.current = key
  }, [props.open, currentRow, isEditing, modelQuery.data, form])

  const save = useMutation({
    meta: { errorToast: false },
    onMutate: () => form.clearErrors('root.server'),
    mutationFn: async (values: ModelFormValues) => {
      const payload = transformFormDataToModelPayload(values)
      return currentRow?.id
        ? updateModel({ ...payload, id: currentRow.id })
        : createModel(payload)
    },
    onSuccess: async (saved) => {
      form.reset(form.getValues())
      if (saved?.id) {
        if (!currentRow?.id) {
          setCreatedModel({ source: props.currentRow, model: saved })
        }
        queryClient.setQueryData(modelsQueryKeys.detail(saved.id), saved)
      }
      await queryClient.invalidateQueries({ queryKey: modelsQueryKeys.all })
      toast.success(t('Model metadata saved'))
      props.onOpenChange(false)
    },
    onError: (error) => {
      form.setError('root.server', {
        message: getServerErrorMessage(error, t('Operation failed')),
      })
    },
  })
  const isSubmitting = save.isPending
  const metadataDirty = form.formState.isDirty
  const close = (open: boolean) => {
    if (!open && isSubmitting) return
    if (!open && metadataDirty) {
      setCloseConfirm(true)
      return
    }
    props.onOpenChange(open)
  }
  // 只取"真实命中的厂商图标"：resolveModelIconKey 的"首字母兜底"只用于渲染，
  // 不能自动写库（否则 example-model 会存下一个无意义的 "e"）。
  const suggestedIcon = useMemo(
    () => resolveModelProvider(watchedModelName ?? '')?.icon ?? '',
    [watchedModelName]
  )

  // ui-spec §6.3：模型名能识别出厂商图标时**自动采用**，用户不必再点「生效图标」。
  // 只在"用户还没手动改过图标"时自动写；一旦手动改过就尊重显式选择，不再覆盖。
  // 采用时 shouldDirty:false：仅凭识别不应把"打开即关闭"标成未保存改动。
  const iconManuallyEdited = useRef(false)
  const lastAutoAppliedIcon = useRef('')
  useEffect(() => {
    if (!props.open) {
      iconManuallyEdited.current = false
      lastAutoAppliedIcon.current = ''
      return
    }
    if (iconManuallyEdited.current) return
    const current = (form.getValues('icon') ?? '').trim()
    if (current && current !== lastAutoAppliedIcon.current) return
    if (!suggestedIcon) return
    if (current !== suggestedIcon) {
      form.setValue('icon', suggestedIcon, { shouldDirty: false })
    }
    lastAutoAppliedIcon.current = suggestedIcon
  }, [props.open, suggestedIcon, form])

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={close}
        size='lg'
        title={hasModelName ? currentRow?.model_name : t('Create Model')}
        description={t(
          'Manage model metadata and view channel associations. Metadata is saved separately.'
        )}
        titleClassName='break-all'
        footer={
          <>
            {form.formState.errors.root?.server?.message && (
              <Alert variant='destructive' className='mr-auto max-w-full'>
                <AlertDescription className='break-words'>
                  {form.formState.errors.root.server.message}
                </AlertDescription>
              </Alert>
            )}
            <Button
              variant='outline'
              onClick={() => close(false)}
              disabled={isSubmitting}
            >
              {t('Close')}
            </Button>
            <Button
              form='model-form'
              type='submit'
              disabled={
                isSubmitting ||
                modelQuery.isError ||
                (isEditing && modelQuery.isPending)
              }
            >
              {isSubmitting ? t('Saving...') : t('Save metadata')}
            </Button>
          </>
        }
      >
        {props.open && (
          <>
            {modelQuery.isError ? (
              <ErrorState
                description={modelQuery.error.message}
                onRetry={() => void modelQuery.refetch()}
              />
            ) : null}
            {isEditing && modelQuery.isPending && <LoadingState />}
            {!modelQuery.isError && !(isEditing && modelQuery.isPending) && (
              <Form {...form}>
                <form
                  id='model-form'
                  onSubmit={form.handleSubmit((values) => save.mutate(values))}
                  className='flex flex-col gap-6'
                >
                  {/* Basic Information */}
                  <SideDrawerSection>
                    <h3 className='text-sm font-semibold'>
                      {t('Basic Information')}
                    </h3>

                    <FormField
                      control={form.control}
                      name='model_name'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel required>{t('Model Name')}</FormLabel>
                          <FormControl>
                            <Input
                              placeholder={t('gpt-4, claude-3-opus, etc.')}
                              {...field}
                            />
                          </FormControl>
                          <FormDescription>
                            {t('The unique identifier for this model')}
                            {isEditing &&
                              watchedModelName !== currentRow?.model_name && (
                                <span className='text-warning mt-1 block'>
                                  {t(
                                    'Renaming metadata does not rename channel models or channel upstream prices. Existing channel prices stay with the original model name.'
                                  )}
                                </span>
                              )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='description'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Description')}</FormLabel>
                          <FormControl>
                            <Textarea
                              placeholder={t('Describe this model...')}
                              rows={3}
                              {...field}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='icon'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Icon')}</FormLabel>
                          <FormControl>
                            <LobeIconField
                              key={`${currentRow?.id ?? 'new'}-${props.open}`}
                              value={field.value ?? ''}
                              onChange={(value) => {
                                iconManuallyEdited.current = true
                                field.onChange(value)
                              }}
                              suggestedIcon={suggestedIcon}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='tags'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Tags')}</FormLabel>
                          <FormControl>
                            <TagInput
                              value={field.value || []}
                              onChange={field.onChange}
                              placeholder={t('Add tags...')}
                            />
                          </FormControl>
                          <FormDescription>
                            {t('Press Enter or comma to add tags')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </SideDrawerSection>

                  {hasModelName && currentRow?.model_name ? (
                    <ModelLinkedChannels
                      modelName={currentRow.model_name}
                      nameRule={currentRow.name_rule}
                      icon={currentRow.icon}
                    />
                  ) : null}
                </form>
              </Form>
            )}
          </>
        )}
      </Dialog>
      <ConfirmDialog
        open={closeConfirm}
        onOpenChange={setCloseConfirm}
        title={t('Discard unsaved changes?')}
        desc={t('Your changes have not been saved.')}
        confirmText={t('Discard changes')}
        handleConfirm={() => {
          setCloseConfirm(false)
          props.onOpenChange(false)
        }}
      />
    </>
  )
}
