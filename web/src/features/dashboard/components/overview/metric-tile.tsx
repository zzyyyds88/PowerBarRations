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
// 首页概览指标条的单个格子（ui-spec §6.2A）。
//
// 一格 = 图标 + 小写标签（CSS 全大写）+ 大数字 + 口径副标题 + 环比。
// 口径副标题说明这个数字"算的是什么"（如"每分钟请求数"），避免只看大数字猜口径。
//
// 环比沿用既有呈现：正数绿升、负数红降、无基准（或持平）为灰；
// 数据缺失时显示 `-`，不伪造 0%。

import { HugeiconsIcon, type IconSvgElement } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { formatDeltaPercent } from '../../lib/format'

export function MetricTile(props: {
  label: string
  value: string
  delta: number | null
  /** 口径副标题：这个数字算的是什么（可选）。 */
  caption?: string
  /** 格子的语义图标（装饰性，标签已给出语义）。 */
  icon?: IconSvgElement
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
    <div
      data-slot='metric-tile'
      className='border-border/70 shadow-control rounded-control bg-muted/50 min-w-0 border px-2.5 py-1.5'
    >
      <div className='flex items-center gap-1.5'>
        {props.icon != null && (
          <HugeiconsIcon
            icon={props.icon}
            size={14}
            strokeWidth={2}
            className='text-muted-foreground shrink-0'
            aria-hidden='true'
          />
        )}
        <span className='text-muted-foreground truncate text-xs font-medium uppercase'>
          {props.label}
        </span>
      </div>
      <div className='mt-0.5 truncate font-mono text-base font-semibold tabular-nums'>
        {props.value}
      </div>
      {props.caption != null && (
        <div
          className='text-muted-foreground mt-0.5 truncate text-xs'
          title={props.caption}
        >
          {props.caption}
        </div>
      )}
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
