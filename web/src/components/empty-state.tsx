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
import { Database, type LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { StatePanel } from '@/components/state-panel'

interface EmptyStateProps {
  icon?: LucideIcon
  title?: string
  description?: string
  /** Creation CTA on pages that own one (ui-spec §12). */
  action?: ReactNode
  className?: string
  bordered?: boolean
}

export function EmptyState(props: EmptyStateProps) {
  const { t } = useTranslation()
  return (
    <StatePanel
      state='empty'
      icon={props.icon ?? Database}
      title={props.title ?? t('No Data')}
      description={props.description}
      action={props.action}
      className={props.className}
      bordered={props.bordered}
    />
  )
}
