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
import { Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'

// ui-spec §6.3：本页只维护模型元数据，是否可调用由车道决定。很多用户会误以为
// "在这里添加模型 = 模型可用"，因此常驻一条指路提示，链到真正的两个落点。
export function ModelsRoleHint() {
  const { t } = useTranslation()

  return (
    <Alert className='border-sky-200 bg-sky-50 text-sky-900 dark:border-sky-500/40 dark:bg-sky-500/10 dark:text-sky-50'>
      <Info className='size-4' aria-hidden='true' />
      <AlertDescription className='flex flex-wrap items-center gap-x-1 gap-y-0.5'>
        <span>
          {t(
            'This page only maintains model metadata. A model is callable only after its lane is saved: declare it in'
          )}
        </span>
        <Link
          to='/channels'
          className='font-medium underline underline-offset-2'
        >
          {t('Channel management')}
        </Link>
        <span>{t('then add members and save in')}</span>
        <Link to='/routes' className='font-medium underline underline-offset-2'>
          {t('Routing & Failover')}
        </Link>
        <span>.</span>
      </AlertDescription>
    </Alert>
  )
}
