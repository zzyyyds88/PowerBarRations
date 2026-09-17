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
import { useTranslation } from 'react-i18next'

import { DIALOG_SIZE_CLASS } from '@/components/dialog-size'
import {
  Dialog as DialogRoot,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

import { ModelRoutingPanel } from './model-routing-panel'

/**
 * 成员链（路由与故障切换）弹窗：行内「编辑成员链」打开时以固定模型模式
 * 渲染 {@link ModelRoutingPanel}，只展示该模型的成员链编辑（居中 Dialog，
 * 参照 ChannelMutateDialog：max-h 约束 + 单层滚动 + header 右上关闭）。
 * 编辑逻辑仍由面板负责。挂在「路由与故障切换」页（ui-spec §6.3）。
 */
export function ModelRoutingDrawer(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 从行内操作打开时点中的模型：弹窗只编辑该模型的成员链。 */
  currentRow?: { model_name?: string } | null
}) {
  const { t } = useTranslation()
  const fixedModel = props.open ? props.currentRow?.model_name : undefined

  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        className={cn(
          'flex w-full flex-col gap-4 overflow-hidden p-4 sm:max-w-none sm:p-6',
          DIALOG_SIZE_CLASS.lg
        )}
      >
        <DialogHeader className='pr-12'>
          <DialogTitle className='flex min-w-0 items-center gap-2'>
            <span className='shrink-0'>{t('Routing & Failover')}</span>
            {fixedModel ? (
              <span
                className='text-muted-foreground min-w-0 truncate font-mono text-sm font-normal'
                title={fixedModel}
              >
                {fixedModel}
              </span>
            ) : null}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Member order is the failover order: requests try the top member first and escape to the next on failure.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex min-h-0 flex-1 flex-col'>
          <ModelRoutingPanel fixedModel={fixedModel} />
        </div>
      </DialogContent>
    </DialogRoot>
  )
}
