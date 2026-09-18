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

import { BadgeCell } from '@/components/data-table'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { getNameRuleConfigByRule } from '../lib'
import type { NameRule } from '../types'

// 「匹配类型」列（ui-spec §6.3）：带色四档标签。label / 颜色 / 说明一律取自
// getNameRuleConfig（经 getNameRuleConfigByRule），本文件不再另写一份 label 表。

const NAME_RULE_VARIANTS: Record<NameRule, StatusVariant> = {
  0: 'green',
  1: 'blue',
  2: 'orange',
  3: 'purple',
}

export function MatchTypeCell(props: { nameRule: number }) {
  const { t } = useTranslation()
  const rule = (props.nameRule || 0) as NameRule
  const config = getNameRuleConfigByRule(rule, t)
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger render={<BadgeCell className='cursor-help' />}>
          <StatusBadge
            label={config.label}
            variant={NAME_RULE_VARIANTS[rule]}
            size='sm'
            copyable={false}
          />
        </TooltipTrigger>
        <TooltipContent side='top' className='max-w-[280px]'>
          {config.description}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
