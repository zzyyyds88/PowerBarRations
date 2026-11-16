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

import type {
  PBRLaneSummaryMember,
  PBRMemberGroup,
  PBRModelSummary,
} from '../api'
import { LaneRuntimeCell } from './lane-runtime-cell'

export interface LaneCardProps {
  summary: PBRModelSummary
  /** 车道成员（有序）。`enabled=false` = 被人工停用，标灰并加短标记（ui-spec §6.3）。 */
  members: PBRLaneSummaryMember[]
  /** 成员组目录（左栏共享数据源）：组引用行默认展开组内成员清单。 */
  groups?: PBRMemberGroup[]
  /** 车道模式；manual 时必须校验 active_member 是否仍能解析到成员。 */
  mode?: string
  activeMember?: string
  snapshot?: LaneHealthSnapshot
  now: number
  onEdit: () => void
  onDelete: () => void
}

/** manual 点名是否仍能解析到当前展开链里的某个成员（含组内成员）。 */
function manualActiveMissing(
  activeMember: string | undefined,
  members: PBRLaneSummaryMember[],
  groups: PBRMemberGroup[] | undefined
): boolean {
  const active = activeMember?.trim()
  if (!active) return true
  const candidates: { alias?: string; label: string }[] = []
  let unresolvedGroup = false
  for (const member of members) {
    if (member.group) {
      const group = groups?.find((item) => item.name === member.group)
      if (!group) {
        unresolvedGroup = true
        continue
      }
      for (const item of group.members) {
        candidates.push({
          alias: item.public_alias,
          label: `${item.channel}/${item.model}`,
        })
      }
      continue
    }
    candidates.push({
      alias: member.public_alias,
      label: `${member.channel}/${member.model}`,
    })
  }
  // 组目录尚未加载完时不要误报“点名失效”；拿到组定义后再判断。
  if (unresolvedGroup) return false
  return !candidates.some(
    (candidate) => candidate.alias === active || candidate.label === active
  )
}

/** manual 点名成员存在，但成员/渠道/所在组被人工停用。 */
function manualActiveDisabled(
  activeMember: string | undefined,
  members: PBRLaneSummaryMember[],
  groups: PBRMemberGroup[] | undefined
): boolean {
  const active = activeMember?.trim()
  if (!active) return false
  for (const member of members) {
    if (member.group) {
      const group = groups?.find((item) => item.name === member.group)
      if (!group) continue
      const groupOff = member.enabled === false || !group.enabled
      for (const item of group.members) {
        const matches =
          active === `${item.channel}/${item.model}` ||
          (item.public_alias !== undefined && item.public_alias === active)
        if (matches && (groupOff || item.enabled === false)) return true
      }
      continue
    }
    const matches =
      active === `${member.channel}/${member.model}` ||
      (member.public_alias !== undefined && member.public_alias === active)
    if (
      matches &&
      (member.enabled === false || member.channel_enabled === false)
    ) {
      return true
    }
  }
  return false
}

export function LaneCard(props: LaneCardProps) {
  const { t } = useTranslation()
  const row = props.summary
  const groups = props.groups
  const unavailable = row.member_count - (row.available_member_count ?? 0)
  // "成员全被我关了"与"上游全挂了"都是 degraded=true，但排障结论完全不同
  // （api-spec §5.7、ui-spec §6.3）：前者靠 disabled_member_count == member_count
  // 判定，文案与配色都必须与"全部成员不可用"区分，不得被它冒充。
  const allMembersDisabled =
    typeof row.disabled_member_count === 'number' &&
    row.member_count > 0 &&
    row.disabled_member_count === row.member_count
  const manualPinMissing =
    props.mode === 'manual' &&
    manualActiveMissing(props.activeMember, props.members, groups)
  const manualPinDisabled =
    props.mode === 'manual' &&
    !manualPinMissing &&
    manualActiveDisabled(props.activeMember, props.members, groups)

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
  } else if (manualPinMissing) {
    statusBadge = (
      <StatusBadge
        label={t('Select active member')}
        variant='warning'
        size='sm'
        copyable={false}
      />
    )
  } else if (manualPinDisabled) {
    statusBadge = (
      <StatusBadge
        label={t('Active member unavailable')}
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

  // 同一组可被引用多次（组不排他）：按出现次序生成稳定 key（组名 # 第 n 次），
  // 避免 no-array-index-key（index 只用于序号显示与排序语义，不作为身份）。
  const groupOccurrence = new Map<string, number>()
  const groupKeyOf = (name: string): string => {
    const n = (groupOccurrence.get(name) ?? 0) + 1
    groupOccurrence.set(name, n)
    return `${name}#${n}`
  }

  return (
    <Card
      data-card-hover='true'
      className='flex min-h-0 flex-col overflow-hidden'
    >
      <CardHeader className='flex-row items-start justify-between gap-2 py-2.5'>
        <div className='flex min-w-0 items-center gap-1'>
          <CardTitle className='truncate font-mono text-sm' title={row.model}>
            {row.model}
          </CardTitle>
          <CopyButton value={row.model} className='size-6 shrink-0' />
        </div>
        {statusBadge}
      </CardHeader>
      <CardContent className='flex min-h-0 flex-1 flex-col gap-2 pt-0'>
        {manualPinMissing && (
          <p className='text-warning text-xs' role='alert'>
            {t(
              'Manual lane has no valid active member. Open Edit members and pick one; requests return no available member until then.'
            )}
          </p>
        )}
        {manualPinDisabled && (
          <p className='text-warning text-xs' role='alert'>
            {t(
              'The active member is disabled or its group is off; requests return no available member.'
            )}
          </p>
        )}
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
                // 组引用元素（ADR 0009）：组名行 + **默认展开**的组内成员清单
                // （ui-spec §6.3，2026-10-25：卡片上直接看到组内有哪些成员）。
                // 组被全局关或本车道引用被关时整组标灰。
                if (member.group) {
                  const groupOff =
                    member.enabled === false || member.group_enabled === false
                  const groupDef = groups?.find((g) => g.name === member.group)
                  let groupTitle: string | undefined
                  if (groupOff) {
                    groupTitle =
                      member.group_enabled === false
                        ? t('Member group is disabled globally')
                        : t('Member group reference is disabled on this lane')
                  }
                  return (
                    <li
                      key={`group\u0000${groupKeyOf(member.group)}`}
                      className={cn('font-mono', groupOff && 'opacity-50')}
                      title={groupTitle}
                    >
                      <div className='truncate'>
                        {index + 1}.{' '}
                        <span className='inline-flex items-center gap-1'>
                          {t('Group')} {member.group}
                          <span className='text-muted-foreground/70'>
                            ({member.group_member_count ?? '-'})
                          </span>
                        </span>
                      </div>
                      <ul className='border-border/60 mt-0.5 ml-4 space-y-0.5 border-l-2 pl-2'>
                        {(groupDef?.members ?? []).map((item) => (
                          <li
                            key={`${item.channel}\u0000${item.model}`}
                            className={cn(
                              'text-muted-foreground/80 truncate text-[11px]',
                              item.enabled === false && 'opacity-50'
                            )}
                            title={`${item.channel} · ${item.model}`}
                          >
                            {item.channel} · {item.model}
                            {item.enabled === false && (
                              <span className='ml-1'>({t('off')})</span>
                            )}
                          </li>
                        ))}
                      </ul>
                    </li>
                  )
                }
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
      <div className='flex items-center justify-end gap-1.5 border-t px-3 py-2'>
        <Button size='sm' variant='outline' onClick={props.onEdit}>
          <Pencil className='size-3.5' />
          {t('Edit members')}
        </Button>
        {/* Destructive action: tinted like every other destructive affordance in
          the console so it does not read as a peer of "Edit members". */}
        <Button
          size='sm'
          variant='ghost'
          className='text-destructive hover:text-destructive'
          onClick={props.onDelete}
        >
          <Trash2 className='size-3.5' />
          {t('Delete lane')}
        </Button>
      </div>
    </Card>
  )
}
