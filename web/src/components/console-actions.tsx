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
import { Link } from '@tanstack/react-router'
import { FlaskConical } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useIsSidebarModuleVisible } from '@/hooks/use-sidebar-config'

/**
 * Global console actions. Playground is not a destination in the 5+1 IA;
 * it is a contextual action available everywhere.
 */
export function ConsoleActions() {
  const { t } = useTranslation()
  const playgroundVisible = useIsSidebarModuleVisible('/playground')

  if (!playgroundVisible) return null

  return (
    <Button
      variant='ghost'
      size='sm'
      className='gap-1.5'
      title={t('Test a route through the gateway')}
      render={<Link to='/playground' />}
    >
      <FlaskConical className='size-4' />
      <span className='hidden lg:inline'>{t('Playground')}</span>
    </Button>
  )
}
