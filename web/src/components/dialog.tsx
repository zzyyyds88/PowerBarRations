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
import * as React from 'react'

import {
  Dialog as DialogRoot,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

/**
 * 居中弹窗统一规范（ui-spec §6.9）。
 *
 * 关键约束：**外框尺寸恒定**——同一档位在任何内容长度、任何页签、任意增删行下
 * 都渲染相同的外框；内容超出只在正文区滚动。禁止调用方再用 ad-hoc 的
 * `max-w-*` / `contentHeight` 撑高弹窗（那正是"一个弹窗一种样式"的根因）。
 */
export type DialogSize = 'sm' | 'md' | 'lg' | 'xl'

/**
 * 各档位固定外框。`sm` 是唯一的自适应档（确认/告警短内容），但仍有最小高度
 * 与统一宽度，避免短文案弹窗忽大忽小。
 *
 * 导出给需要自定义 header/footer 结构、因而直接使用 `ui/dialog` 的少数调用方，
 * 以保证"尺寸只有一个来源"（ui-spec §6.9）。
 */
export const DIALOG_SIZE_CLASS: Record<DialogSize, string> = {
  sm: 'w-[min(92vw,480px)] min-h-[160px] max-h-[min(86vh,560px)]',
  md: 'w-[min(92vw,720px)] h-[min(78vh,560px)]',
  lg: 'w-[min(94vw,960px)] h-[min(82vh,640px)]',
  xl: 'w-[min(96vw,1200px)] h-[min(86vh,720px)]',
}

type DialogProps = React.ComponentProps<typeof DialogRoot> & {
  title: React.ReactNode
  description?: React.ReactNode
  children: React.ReactNode
  trigger?: React.ReactElement
  footer?: React.ReactNode
  /** 固定外框档位（ui-spec §6.9）。默认 `md`。 */
  size?: DialogSize
  contentClassName?: string
  headerClassName?: string
  titleClassName?: string
  descriptionClassName?: string
  bodyClassName?: string
  footerClassName?: string
  initialFocus?: boolean
  showCloseButton?: boolean
}

const dialogContentMotionClassName =
  'data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95 duration-100'

export function Dialog({
  title,
  description,
  children,
  trigger,
  footer,
  size = 'md',
  contentClassName,
  headerClassName,
  titleClassName,
  descriptionClassName,
  bodyClassName,
  footerClassName,
  initialFocus,
  showCloseButton,
  ...dialogProps
}: DialogProps) {
  const fills = size !== 'sm'
  return (
    <DialogRoot {...dialogProps}>
      {trigger ? <DialogTrigger render={trigger} /> : null}
      <DialogContent
        className={cn(
          'flex w-full flex-col gap-4 overflow-hidden p-4 sm:max-w-none sm:p-6',
          DIALOG_SIZE_CLASS[size],
          contentClassName,
          dialogContentMotionClassName
        )}
        initialFocus={initialFocus}
        showCloseButton={showCloseButton}
      >
        <DialogHeader
          className={cn('flex-shrink-0 text-start', headerClassName)}
        >
          <DialogTitle className={titleClassName}>{title}</DialogTitle>
          {description ? (
            <DialogDescription className={descriptionClassName}>
              {description}
            </DialogDescription>
          ) : null}
        </DialogHeader>

        <div
          className={cn(
            '-mx-1 min-h-0 overflow-x-hidden overflow-y-auto overscroll-contain',
            fills ? 'flex-1' : ''
          )}
        >
          <div
            className={cn(
              'min-w-0 px-1 py-1',
              '[&_form]:overflow-x-visible',
              '[&_[data-slot=scroll-area-viewport]]:px-1 [&_[data-slot=scroll-area-viewport]]:py-1',
              bodyClassName
            )}
          >
            {children}
          </div>
        </div>

        {footer ? (
          <DialogFooter
            className={cn(
              'flex-shrink-0 gap-2 sm:-mx-6 sm:-mb-6 sm:justify-end sm:p-6',
              footerClassName
            )}
          >
            {footer}
          </DialogFooter>
        ) : null}
      </DialogContent>
    </DialogRoot>
  )
}
