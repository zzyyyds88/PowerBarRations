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
import { AlertTriangle, type LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { StatePanel } from '@/components/state-panel'
import { Button } from '@/components/ui/button'

interface ErrorStateProps {
  icon?: LucideIcon
  title?: string
  description?: string
  /** Retry is the expected action for a failed query (ui-spec §12). */
  onRetry?: () => void
  action?: ReactNode
  className?: string
}

export function ErrorState(props: ErrorStateProps) {
  const { t } = useTranslation()
  return (
    <StatePanel
      state='error'
      tone='danger'
      icon={props.icon ?? AlertTriangle}
      title={props.title ?? t('Oops! Something went wrong')}
      description={props.description}
      className={props.className}
      action={
        props.onRetry != null || props.action != null ? (
          <>
            {props.onRetry != null && (
              <Button variant='outline' size='sm' onClick={props.onRetry}>
                {t('Retry')}
              </Button>
            )}
            {props.action}
          </>
        ) : undefined
      }
    />
  )
}
