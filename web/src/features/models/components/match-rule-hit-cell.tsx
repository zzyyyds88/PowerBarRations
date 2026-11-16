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

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

import { getNameRuleConfigByRule } from '../lib'
import type { NameRule } from '../types'

// 模型名内联「规则 · 命中数」（ui-spec §6.3）：只有非精确条目才有命中集，
// 规则 label 复用 getNameRuleConfigByRule，其值来自控制台面 matched_count；
// 点击数字弹出 matched_models 的具体模型名清单。精确条目命中数恒为自身，
// 不渲染本组件。

export function MatchRuleHitCell(props: {
  nameRule: number
  matchedCount?: number
  matchedModels?: string[]
}) {
  const { t } = useTranslation()
  if (props.nameRule === 0) {
    return null
  }
  const config = getNameRuleConfigByRule(props.nameRule as NameRule, t)
  const models = props.matchedModels ?? []
  const count = props.matchedCount ?? models.length
  if (count === 0) {
    return (
      <span className='text-muted-foreground shrink-0'>{config.label} · 0</span>
    )
  }
  return (
    <span className='text-muted-foreground flex shrink-0 items-center gap-1'>
      {config.label} ·
      <Popover>
        <PopoverTrigger
          render={
            <Button
              variant='link'
              size='sm'
              className='h-auto p-0 text-xs font-medium'
              aria-label={t('View matched models')}
            />
          }
        >
          {count}
        </PopoverTrigger>
        <PopoverContent
          align='start'
          aria-label={t('Matched models')}
          className='max-h-64 w-72 overflow-y-auto'
        >
          <p className='text-muted-foreground mb-2 text-xs'>
            {t('Matched models ({{count}})', { count })}
          </p>
          <ul className='flex flex-col gap-1'>
            {models.map((name) => (
              <li key={name}>
                <StatusBadge label={name} size='sm' variant='neutral' />
              </li>
            ))}
          </ul>
        </PopoverContent>
      </Popover>
    </span>
  )
}
