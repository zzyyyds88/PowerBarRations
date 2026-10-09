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
// 「模型数据分析」卡的指标格（ui-spec §6.2A）。
//
// 值 + 与上一等长窗口的环比：正数绿升、负数红降、无基准（或持平）为灰。
// 数据缺失时环比显示 `-`，不伪造 0%。

import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { formatDeltaPercent } from '../../lib/format'

export function MetricTile(props: {
  label: string
  value: string
  delta: number | null
}) {
  const { t } = useTranslation()

  let tone = 'text-muted-foreground'
  let arrow = ''
  if (props.delta != null && props.delta > 0) {
    tone = 'text-success'
    arrow = '↑'
  } else if (props.delta != null && props.delta < 0) {
    tone = 'text-destructive'
    arrow = '↓'
  }

  return (
    <div className='bg-muted/60 rounded-control min-w-0 px-2.5 py-1.5'>
      <div className='text-muted-foreground truncate text-xs font-medium'>
        {props.label}
      </div>
      <div className='mt-0.5 truncate font-mono text-base font-semibold tabular-nums'>
        {props.value}
      </div>
      <div className={cn('mt-0.5 flex items-center gap-1 text-xs', tone)}>
        <span aria-hidden='true'>{arrow}</span>
        <span className='shrink-0 tabular-nums'>
          {formatDeltaPercent(props.delta)}
        </span>
        <span className='text-muted-foreground truncate'>
          {t('vs previous period')}
        </span>
      </div>
    </div>
  )
}
