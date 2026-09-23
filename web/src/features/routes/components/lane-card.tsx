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
/*
车道卡片（ui-spec §6.3、ADR 0006/0007）：/routes 页以卡片网格展示每条车道。
路由页只列真实车道，卡片操作固定为「编辑成员链 / 删除车道」。
*/
import { Pencil, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { LaneHealthSnapshot } from '@/lib/route-events'
import { cn } from '@/lib/utils'

import type { PBRLaneSummaryMember, PBRModelSummary } from '../api'
import { LaneRuntimeCell } from './lane-runtime-cell'

export interface LaneCardProps {
  summary: PBRModelSummary
  /** 车道成员（有序）。`enabled=false` = 被人工停用，标灰并加短标记（ui-spec §6.3）。 */
  members: PBRLaneSummaryMember[]
  snapshot?: LaneHealthSnapshot
  now: number
  onEdit: () => void
  onDelete: () => void
}

export function LaneCard(props: LaneCardProps) {
  const { t } = useTranslation()
  const row = props.summary
  const unavailable = row.member_count - (row.available_member_count ?? 0)
  // "成员全被我关了"与"上游全挂了"都是 degraded=true，但排障结论完全不同
  // （api-spec §5.7、ui-spec §6.3）：前者靠 disabled_member_count == member_count
  // 判定，文案与配色都必须与"全部成员不可用"区分，不得被它冒充。
  const allMembersDisabled =
    typeof row.disabled_member_count === 'number' &&
    row.member_count > 0 &&
    row.disabled_member_count === row.member_count

  let statusBadge
  if (row.source === 'disabled') {
    statusBadge = (
      <StatusBadge
        label={t('Lane disabled')}
        variant='warning'
        size='sm'
        copyable={false}
      />
    )
  } else if (allMembersDisabled) {
    statusBadge = (
      <StatusBadge
        label={t('All members disabled')}
        variant='neutral'
        size='sm'
        copyable={false}
      />
    )
  } else if (row.degraded) {
    statusBadge = (
      <StatusBadge
        label={t('All members unavailable')}
        variant='danger'
        size='sm'
        copyable={false}
      />
    )
  } else {
    const partial =
      typeof row.healthy_member_count === 'number' &&
      typeof row.health_member_count === 'number' &&
      row.health_member_count > 0 &&
      row.healthy_member_count < row.health_member_count
    statusBadge = partial ? (
      <StatusBadge
        label={t('Degraded')}
        variant='warning'
        size='sm'
        copyable={false}
      />
    ) : (
      <StatusBadge
        label={t('Callable')}
        variant='success'
        size='sm'
        copyable={false}
      />
    )
  }

  return (
    <Card className='flex min-h-0 flex-col overflow-hidden'>
      <CardHeader className='flex-row items-start justify-between gap-2 py-3'>
        <div className='flex min-w-0 items-center gap-1'>
          <CardTitle className='truncate font-mono text-sm' title={row.model}>
            {row.model}
          </CardTitle>
          <CopyButton value={row.model} className='size-6 shrink-0' />
        </div>
        {statusBadge}
      </CardHeader>
      <CardContent className='flex min-h-0 flex-1 flex-col gap-2 pt-0'>
        <div className='text-xs'>
          <div className='text-sm'>
            {t('{{count}} members', { count: row.member_count })}
            {unavailable > 0 && (
              <span className='text-destructive ml-1 text-xs'>
                {t('({{count}} unavailable)', { count: unavailable })}
              </span>
            )}
          </div>
          {props.members.length > 0 && (
            <ol className='text-muted-foreground mt-1 space-y-0.5'>
              {props.members.map((member, index) => {
                // 被人工停用的成员仍留在链里（开关不是删除别名），但要可见地标灰，
                // 否则卡片看起来"一切正常"而实际不参与选路（ui-spec §6.3）。
                const disabled = member.enabled === false
                // 成员身份 = 所选模型（ADR 0008）；`upstream_model` 是它经渠道映射
                // 派生出的只读真名，仅在改名时补出来（两者相同就不重复占位）。
                const renamed = member.upstream_model !== member.model
                return (
                  <li
                    key={`${member.channel}\u0000${member.model}`}
                    className={cn(
                      'truncate font-mono',
                      disabled && 'opacity-50'
                    )}
                    title={`${member.channel} / ${member.model}${
                      renamed ? ` → ${member.upstream_model}` : ''
                    }`}
                  >
                    {index + 1}. {member.channel} · {member.model}
                    {renamed && (
                      <span className='text-muted-foreground/70'>
                        {' '}
                        → {member.upstream_model}
                      </span>
                    )}
                    {disabled && (
                      <StatusBadge
                        className='ml-1 align-middle'
                        label={t('Manually disabled')}
                        variant='neutral'
                        size='sm'
                        copyable={false}
                      />
                    )}
                  </li>
                )
              })}
            </ol>
          )}
        </div>

        <div className='mt-auto'>
          <LaneRuntimeCell snapshot={props.snapshot} now={props.now} />
        </div>
      </CardContent>
      <div className='flex items-center justify-end gap-2 border-t px-4 py-2'>
        <Button size='sm' variant='outline' onClick={props.onEdit}>
          <Pencil className='size-3.5' />
          {t('Edit members')}
        </Button>
        <Button size='sm' variant='ghost' onClick={props.onDelete}>
          <Trash2 className='size-3.5' />
          {t('Delete lane')}
        </Button>
      </div>
    </Card>
  )
}
