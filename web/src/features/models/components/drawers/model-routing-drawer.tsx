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

import {
  sideDrawerContentClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

import type { Model } from '../../types'
import { ModelRoutingPanel } from '../model-routing-panel'

/**
 * 成员链（路由与故障切换）抽屉：仅作为既有 {@link ModelRoutingPanel} 的
 * 抽屉容器，编辑逻辑仍由面板负责（W7 再手工化）。
 */
export function ModelRoutingDrawer(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 从行内操作打开时点中的模型：面板据此预选该模型的成员链。 */
  currentRow?: Model | null
}) {
  const { t } = useTranslation()

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[1100px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>{t('Routing & Failover')}</SheetTitle>
          <SheetDescription>
            {t(
              'Member order is the failover order: requests try the top member first and escape to the next on failure.'
            )}
          </SheetDescription>
        </SheetHeader>
        <ModelRoutingPanel
          initialModel={props.open ? props.currentRow?.model_name : undefined}
        />
      </SheetContent>
    </Sheet>
  )
}
