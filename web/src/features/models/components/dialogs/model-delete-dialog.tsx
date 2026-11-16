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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { isAxiosError } from 'axios'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { pbrModelsQueryKey } from '@/features/routes/api'
import { ROLE } from '@/lib/roles'
import { getServerErrorDetails } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { deleteModel, deleteModels, type ModelDeleteResult } from '../../api'
import { modelsQueryKeys } from '../../lib'
import type { Model } from '../../types'

interface ModelDeleteDialogProps {
  models: Pick<Model, 'id' | 'model_name' | 'name_rule'>[]
  onClose: () => void
  onSuccess?: () => void
}

/** 冲突明细里的 groups 可能是数组，也可能是 渠道名 → 组名[] 的映射。 */
function flattenGroupNames(value: unknown): string[] {
  const out = new Set<string>()
  const visit = (item: unknown) => {
    if (typeof item === 'string') {
      if (item.trim()) out.add(item)
      return
    }
    if (Array.isArray(item)) {
      item.forEach(visit)
      return
    }
    if (item && typeof item === 'object') {
      Object.values(item as Record<string, unknown>).forEach(visit)
    }
  }
  visit(value)
  return [...out]
}

export function ModelDeleteDialog(props: ModelDeleteDialogProps) {
  const { t } = useTranslation()
  const checkboxId = useId()
  const pricingCheckboxId = useId()
  const canEditPricing = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  const supportsChannelRemoval = props.models.every(
    (model) => model.name_rule === 0
  )
  const [removePricing, setRemovePricing] = useState(false)
  const [removeFromChannels, setRemoveFromChannels] = useState(false)
  const [laneConflict, setLaneConflict] = useState<Record<
    string,
    string[]
  > | null>(null)
  const [groupConflict, setGroupConflict] = useState<string[]>([])
  const client = useQueryClient()
  const mutation = useMutation<ModelDeleteResult, Error, boolean>({
    mutationFn: async (force = false): Promise<ModelDeleteResult> => {
      const ids = props.models.map((model) => model.id)
      setLaneConflict(null)
      setGroupConflict([])
      try {
        if (ids.length === 1) {
          return await deleteModel(
            ids[0],
            removeFromChannels && supportsChannelRemoval,
            removePricing && canEditPricing,
            force
          )
        }
        return await deleteModels(
          ids,
          removeFromChannels && supportsChannelRemoval,
          removePricing && canEditPricing,
          force
        )
      } catch (error) {
        // 被车道/成员组引用：明细在 error.details（api-spec §3）。成员组必须
        // 先编辑，散成员可以由用户确认后 force 清理，二者不能混为一谈。
        const details = getServerErrorDetails(error)
        if (details.code === 'conflict') {
          const raw = details.details as
            | {
                blocked?: Record<string, string[]>
                groups?: unknown
                blocked_groups?: unknown
              }
            | undefined
          if (raw?.blocked) setLaneConflict(raw.blocked)
          const groups = flattenGroupNames(raw?.groups ?? raw?.blocked_groups)
          if (groups.length > 0) setGroupConflict(groups)
        }
        throw error
      }
    },
    onSuccess: async (result) => {
      await client.invalidateQueries({ queryKey: modelsQueryKeys.lists() })
      // 路由页的可调用状态由车道/渠道成员决定，删除模型后必须失效，否则仍显示可调用。
      await client.invalidateQueries({ queryKey: pbrModelsQueryKey })
      if (removePricing) {
        await client.invalidateQueries({ queryKey: ['system-options'] })
      }
      if (removeFromChannels) {
        await client.invalidateQueries({ queryKey: ['channels'] })
        await client.invalidateQueries({ queryKey: ['pbr-route'] })
        await client.invalidateQueries({ queryKey: ['pbr-channel-catalog'] })
      }
      const details: string[] = []
      if (
        (result.cleaned_lanes?.length ?? 0) > 0 ||
        (result.deleted_lanes?.length ?? 0) > 0
      ) {
        details.push(
          t(
            'Removed this channel from {{cleaned}} lane(s); deleted {{deleted}} empty lane(s). Those models are no longer callable.',
            {
              cleaned: result.cleaned_lanes?.length ?? 0,
              deleted: result.deleted_lanes?.length ?? 0,
            }
          )
        )
      }
      if ((result.manual_lanes_cleared?.length ?? 0) > 0) {
        details.push(
          t('Manual lanes now need an active member: {{lanes}}.', {
            lanes: (result.manual_lanes_cleared ?? []).join(', '),
          })
        )
      }
      toast.success(
        t('Successfully deleted {{count}} model(s)', {
          count: result.deleted_count,
        }),
        details.length > 0 ? { description: details.join(' ') } : undefined
      )
      props.onSuccess?.()
      props.onClose()
    },
  })
  const description =
    props.models.length === 1
      ? t('Delete model "{{name}}"?', { name: props.models[0].model_name })
      : t('Delete {{count}} models?', { count: props.models.length })
  let errorMessage = mutation.error?.message
  if (isAxiosError<{ message?: string }>(mutation.error)) {
    errorMessage = mutation.error.response?.data.message || errorMessage
  }
  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) props.onClose()
      }}
      title={t('Delete Models?')}
      desc={description}
      confirmText={t('Delete')}
      destructive
      disabled={!props.models.length || groupConflict.length > 0}
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate(false)}
    >
      <div className='space-y-3'>
        <div className='flex items-start gap-2'>
          <Checkbox
            id={checkboxId}
            className='mt-0.5'
            checked={removeFromChannels && supportsChannelRemoval}
            disabled={mutation.isPending || !supportsChannelRemoval}
            onCheckedChange={(checked) => {
              setRemoveFromChannels(checked === true)
              // 取消“从渠道移除”后，之前的车道/组冲突不再适用；保留它们会
              // 让只删目录的安全操作被旧冲突锁死。
              setLaneConflict(null)
              setGroupConflict([])
            }}
          />
          <Label htmlFor={checkboxId} className='flex-wrap leading-normal'>
            {t('Also remove from all channels')}
            {!supportsChannelRemoval && (
              <span className='text-muted-foreground text-xs font-normal'>
                {t('Only available for exact matching')}
              </span>
            )}
          </Label>
        </div>
        {removeFromChannels && supportsChannelRemoval && (
          <p className='text-muted-foreground text-sm'>
            {t(
              'Lanes that reference these models will have this channel member removed; lanes left without members are deleted, and those models become uncallable.'
            )}
          </p>
        )}
        <div className='flex items-start gap-2'>
          <Checkbox
            id={pricingCheckboxId}
            className='mt-0.5'
            checked={removePricing}
            disabled={mutation.isPending || !canEditPricing}
            onCheckedChange={(checked) => setRemovePricing(checked === true)}
          />
          <Label htmlFor={pricingCheckboxId} className='leading-normal'>
            {t('Also remove pricing')}
          </Label>
        </div>
        {(removePricing || !canEditPricing) && (
          <p className='text-muted-foreground text-sm'>
            {canEditPricing
              ? t('Built-in pricing may become effective again.')
              : t('Model pricing is managed by a super administrator.')}
          </p>
        )}
        {groupConflict.length > 0 && (
          <Alert variant='destructive'>
            <AlertDescription>
              <p>
                {t(
                  'These models are still used by member groups. Edit those groups first:'
                )}
              </p>
              <ul className='mt-2 list-disc space-y-1 ps-5'>
                {groupConflict.map((group) => (
                  <li key={group}>
                    <Link
                      to='/routes'
                      search={{ group }}
                      className='underline underline-offset-2'
                    >
                      {group}
                    </Link>
                  </li>
                ))}
              </ul>
              <Link
                to='/routes'
                search={{}}
                className='mt-3 inline-block font-medium underline underline-offset-2'
              >
                {t('Routing & Failover')}
              </Link>
            </AlertDescription>
          </Alert>
        )}
        {laneConflict && Object.keys(laneConflict).length > 0 && (
          <Alert variant='destructive'>
            <AlertDescription>
              <p>
                {t(
                  'These models are still referenced by lanes on the following channels:'
                )}
              </p>
              <ul className='mt-2 space-y-2'>
                {Object.entries(laneConflict).map(([name, lanes]) => (
                  <li key={name}>
                    <span className='font-medium'>{name}</span>
                    <ul className='list-disc space-y-1 ps-5'>
                      {lanes.map((lane) => (
                        <li key={lane}>
                          <Link
                            to='/routes'
                            search={{ model: lane }}
                            className='underline underline-offset-2'
                          >
                            {lane}
                          </Link>
                        </li>
                      ))}
                    </ul>
                  </li>
                ))}
              </ul>
              {groupConflict.length === 0 &&
                removeFromChannels &&
                supportsChannelRemoval && (
                  <Button
                    type='button'
                    size='sm'
                    variant='outline'
                    className='mt-3'
                    disabled={mutation.isPending}
                    onClick={() => mutation.mutate(true)}
                  >
                    {t('Remove from these lanes and delete')}
                  </Button>
                )}
              <Link
                to='/routes'
                search={{}}
                className='mt-3 inline-block font-medium underline underline-offset-2'
              >
                {t('Routing & Failover')}
              </Link>
            </AlertDescription>
          </Alert>
        )}
        {mutation.isError && !laneConflict && groupConflict.length === 0 && (
          <p role='alert' className='text-destructive text-sm'>
            {errorMessage || t('Failed to delete model')}
          </p>
        )}
      </div>
    </ConfirmDialog>
  )
}
