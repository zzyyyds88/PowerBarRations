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

import { cn } from '@/lib/utils'

import { formatCostYuan } from '../lib/cost'
import type { ApiKey } from '../types'

// 令牌「消耗」（ui-spec §6.5 / token-spec §3.7）：该令牌的上游折算花费。
// 数据来自 GET /api/keys 的只读 cost（元），与看板/日志同源，不读明细，
// 故 logs/prune 后该值不消失。此处只展示金额，不含进度条或百分比。

type ApiKeyCostCellProps = {
  apiKey: ApiKey
  variant?: 'table' | 'card'
}

export function ApiKeyCostCell(props: ApiKeyCostCellProps) {
  const { t } = useTranslation()
  const formatted = formatCostYuan(props.apiKey.cost)

  if (props.variant === 'card') {
    return (
      <div className='flex min-w-0 items-center justify-between gap-2 text-xs'>
        <span className='text-muted-foreground'>{t('Consumed')} (¥)</span>
        <span
          data-slot='api-key-cost'
          className='min-w-0 truncate text-right tabular-nums'
          title={formatted}
        >
          {formatted}
        </span>
      </div>
    )
  }

  return (
    <span
      data-slot='api-key-cost'
      className={cn('block min-w-0 text-left font-medium tabular-nums')}
      title={formatted}
    >
      {formatted}
    </span>
  )
}
