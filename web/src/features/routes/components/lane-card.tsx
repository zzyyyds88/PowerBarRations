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
车道卡片（ui-spec §6.3、ADR 0006）：/routes 页以卡片网格展示每条车道。
卡面给出路由键、状态徽章、成员顺序摘要与运行态；操作 = 编辑成员链 / 删除车道。
*/
import { Pencil, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { LaneHealthSnapshot } from '@/lib/route-events'

import type { PBRModelSummary } from '../api'
import { LaneRuntimeCell } from './lane-runtime-cell'

export interface LaneCardProps {
  summary: PBRModelSummary
  /** 车道成员（有序；未配车道时为空）。 */
  members: { channel: string; upstream_model: string }[]
  snapshot?: LaneHealthSnapshot
  now: number
  onEdit: () => void
  onDelete: () => void
}

export function LaneCard(props: LaneCardProps) {
  const { t } = useTranslation()
  const row = props.summary
  const editing = row.source !== 'unconfigured'
  const unavailable = row.member_count - (row.available_member_count ?? 0)

  let statusBadge
  if (row.source === 'explicit') {
    if (row.degraded) {
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
  } else if (row.source === 'disabled') {
    statusBadge = (
      <StatusBadge
        label={t('Lane disabled')}
        variant='warning'
        size='sm'
        copyable={false}
      />
    )
  } else {
    statusBadge = (
      <StatusBadge
        label={t('Not callable')}
        variant='danger'
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
          {editing ? (
            <>
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
                  {props.members.map((member, index) => (
                    <li
                      key={`${member.channel}\u0000${member.upstream_model}`}
                      className='truncate font-mono'
                      title={`${member.channel} / ${member.upstream_model}`}
                    >
                      {index + 1}. {member.channel} · {member.upstream_model}
                    </li>
                  ))}
                </ol>
              )}
            </>
          ) : (
            <span className='text-muted-foreground'>
              {t('{{count}} candidate channels', { count: row.member_count })}
              {' · '}
              {t('No lane · not callable')}
            </span>
          )}
        </div>

        {editing && (
          <div className='mt-auto'>
            <LaneRuntimeCell snapshot={props.snapshot} now={props.now} />
          </div>
        )}
      </CardContent>
      <div className='flex items-center justify-end gap-2 border-t px-4 py-2'>
        <Button size='sm' variant='outline' onClick={props.onEdit}>
          <Pencil className='size-3.5' />
          {editing ? t('Edit members') : t('Create lane')}
        </Button>
        {editing && (
          <Button size='sm' variant='ghost' onClick={props.onDelete}>
            <Trash2 className='size-3.5' />
            {t('Delete lane')}
          </Button>
        )}
      </div>
    </Card>
  )
}
