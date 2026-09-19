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
/*
车道编辑弹窗（ui-spec §6.3、ADR 0006）：新建车道与编辑成员链共用。
- 新建：model 为空，路由键可编辑，成员从空开始。
- 编辑：model 为车道名，载入真实成员链与六键，路由键只读。

关闭时若有未保存草稿，先确认放弃（复用模型编辑弹窗的 Discard 文案）。
*/
import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DIALOG_SIZE_CLASS } from '@/components/dialog-size'
import {
  Dialog as DialogRoot,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

import { getPBRRoute, listPBRChannelCatalog, type PBRLaneMode } from '../api'
import { LaneComposer, type ComposerMember } from './lane-composer'

export function LaneEditorDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 空 = 新建车道；非空 = 编辑该车道的成员链。 */
  model?: string
  /** 新建模式下的路由键初值（从"未配车道"卡片进入时预填）。 */
  prefillName?: string
  onSaved: () => Promise<void> | void
}) {
  const { t } = useTranslation()
  const [dirty, setDirty] = useState(false)
  const [discardOpen, setDiscardOpen] = useState(false)
  const editing = Boolean(props.model)

  const routeQuery = useQuery({
    queryKey: ['pbr-route', props.model, 'composer'],
    queryFn: () => getPBRRoute(props.model ?? ''),
    enabled: props.open && editing,
  })
  // 渠道目录与编排器共用同一 queryKey，避免重复请求。
  useQuery({
    queryKey: ['pbr-channel-catalog'],
    queryFn: listPBRChannelCatalog,
    enabled: props.open,
  })

  const handleOpenChange = (open: boolean) => {
    if (!open && dirty) {
      setDiscardOpen(true)
      return
    }
    if (!open) setDirty(false)
    props.onOpenChange(open)
  }

  // 只有真正存在车道（explicit/disabled）才预载成员；未配车道时后端返回的
  // members 是"推荐链"，按 ADR 0006 不得自动填入，必须从空列表开始。
  const hasLane =
    routeQuery.data?.source === 'explicit' ||
    routeQuery.data?.source === 'disabled'
  const initialMembers: ComposerMember[] = hasLane
    ? (routeQuery.data?.members ?? []).map((m, index) => ({
        id: `initial-${index}`,
        channel: m.channel,
        upstreamOverride: m.upstream_override ?? '',
        resolvedUpstream: m.upstream_model,
        publicAlias: m.public_alias,
      }))
    : []
  const initialMode: PBRLaneMode =
    hasLane && routeQuery.data?.mode === 'manual' ? 'manual' : 'failover'
  const initialActive = hasLane ? (routeQuery.data?.active_member ?? '') : ''

  let body = null
  if (editing && routeQuery.isLoading) {
    body = (
      <div className='text-muted-foreground flex items-center gap-2 text-sm'>
        <Loader2 className='size-4 animate-spin' /> {t('Loading...')}
      </div>
    )
  } else {
    body = (
      <LaneComposer
        key={props.model ?? props.prefillName ?? 'new'}
        model={props.model}
        initialName={props.prefillName}
        initialMembers={initialMembers}
        initialMode={initialMode}
        initialActiveMember={initialActive}
        initialConfig={routeQuery.data?.config ?? null}
        onDirtyChange={setDirty}
        onSaved={async () => {
          setDirty(false)
          await props.onSaved()
          props.onOpenChange(false)
        }}
        onCancel={() => handleOpenChange(false)}
      />
    )
  }

  return (
    <>
      <DialogRoot open={props.open} onOpenChange={handleOpenChange}>
        <DialogContent
          className={cn(
            'flex w-full flex-col gap-4 overflow-hidden p-4 sm:max-w-none sm:p-6',
            DIALOG_SIZE_CLASS.xl
          )}
        >
          <DialogHeader className='pr-12'>
            <DialogTitle className='flex min-w-0 items-center gap-2'>
              <span className='shrink-0'>
                {editing ? t('Edit lane') : t('New lane')}
              </span>
              {props.model ? (
                <span
                  className='text-muted-foreground min-w-0 truncate font-mono text-sm font-normal'
                  title={props.model}
                >
                  {props.model}
                </span>
              ) : null}
            </DialogTitle>
            <DialogDescription>
              {t(
                'Pick member channels and models from any channel, then order them. The order is the failover order.'
              )}
            </DialogDescription>
          </DialogHeader>
          <div className='flex min-h-0 flex-1 flex-col'>{body}</div>
        </DialogContent>
      </DialogRoot>

      <ConfirmDialog
        open={discardOpen}
        onOpenChange={setDiscardOpen}
        title={t('Discard unsaved changes?')}
        desc={t('Your changes have not been saved.')}
        confirmText={t('Discard changes')}
        handleConfirm={() => {
          setDiscardOpen(false)
          setDirty(false)
          props.onOpenChange(false)
        }}
      />
    </>
  )
}
