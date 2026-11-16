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
抽屉第 1 块「当前状态」（ui-spec §6.4-1）：启用/停用 + 协议徽章、被哪些车道引用
（车道/成员/优先级/是否人工停用）、成员运行态表（车道/成员/可用/冷却到期/熔断态/
连续失败/滚动成功率/最近错误分类）。字段口径 api-spec §6.12（与车道健康快照 §6.5 同义）。
协议徽章与状态页「协议」列共用 resolveProtocolLabelKey（控制台渠道级协议优先，
契约适配器名兜底）；`enabled=false` 与故障是两件事，因此人工停用单独用中性徽章标出。
*/
import { Activity } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import {
  circuitVariant,
  formatStatusTime,
  formatSuccessRate,
  hasUpstreamReject,
  isCooldownActive,
  resolveProtocolLabelKey,
} from '../../lib/channel-status-display'
import type { ChannelStatusDetail } from '../../status-api'
import type { Channel } from '../../types'

/** 单元格占位符：无值时统一 '-' 语义（与列表页的占位一致）。 */
function EmptyCell() {
  return <span className='text-muted-foreground text-xs'>-</span>
}

/**
 * 熔断态文案：保持 t() 字面量以便 i18n 扫描（与 lane-runtime-cell 的
 * stateLabel 同套路），未知取值按原样展示。
 */
function circuitLabel(t: (key: string) => string, circuit: string): string {
  if (circuit === 'open') return t('Circuit open')
  if (circuit === 'half-open') return t('Half-open')
  if (circuit === 'closed') return t('Closed')
  return circuit
}

export function ChannelStatusCurrentSection(props: {
  detail: ChannelStatusDetail
  channel: Channel
  now: number
}) {
  const { t } = useTranslation()
  const detail = props.detail
  const runtime = detail.runtime
  const lanes = detail.reference.lanes
  // 协议徽章：控制台记录的渠道级协议优先，缺失时按契约的适配器名兜底
  // （与状态页「协议」列同一个解析，两处说法一致）。
  const protocolLabelKey = resolveProtocolLabelKey(
    props.channel,
    detail.channel.type
  )
  const upstreamRejected = hasUpstreamReject(
    runtime.members.map((member) => member.last_error_kind)
  )

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader
        title={t('Current status')}
        icon={<Activity className='size-4' />}
        iconTone='info'
      />

      <div className='flex flex-wrap items-center gap-2'>
        <StatusBadge
          label={detail.channel.enabled ? t('Enabled') : t('Disabled')}
          variant={detail.channel.enabled ? 'success' : 'neutral'}
          size='sm'
          copyable={false}
        />
        {protocolLabelKey ? (
          <Badge variant='secondary'>{t(protocolLabelKey)}</Badge>
        ) : (
          <Badge variant='secondary'>
            {detail.channel.type || t('Unknown')}
          </Badge>
        )}
        <span className='text-muted-foreground text-xs'>
          {t('Available members: {{available}}/{{total}}', {
            available: runtime.available_count,
            total: runtime.member_count,
          })}
        </span>
      </div>

      <div className='flex flex-col gap-2'>
        <h4 className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
          {t('Referenced lanes')}
        </h4>
        {lanes.length === 0 ? (
          <p className='text-muted-foreground text-sm'>
            {t('Not referenced by any lane')}
          </p>
        ) : (
          <Table className='text-xs'>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Lane')}</TableHead>
                <TableHead>{t('Member')}</TableHead>
                <TableHead>{t('Priority')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {lanes.map((reference) => (
                <TableRow
                  key={`${reference.lane}/${reference.model}/${reference.priority}`}
                >
                  <TableCell className='font-medium'>
                    {reference.lane}
                  </TableCell>
                  <TableCell>
                    <span className='flex min-w-0 flex-col'>
                      <span className='truncate'>{reference.model}</span>
                      {reference.upstream_model !== reference.model && (
                        <span
                          data-table-text='secondary'
                          className='text-muted-foreground truncate font-mono'
                        >
                          {reference.upstream_model}
                        </span>
                      )}
                    </span>
                  </TableCell>
                  <TableCell className='tabular-nums'>
                    {reference.priority}
                  </TableCell>
                  <TableCell>
                    {reference.enabled ? (
                      <span className='text-muted-foreground text-xs'>
                        {t('Enabled')}
                      </span>
                    ) : (
                      <StatusBadge
                        label={t('Manually disabled')}
                        variant='neutral'
                        size='sm'
                        copyable={false}
                      />
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      <div className='flex flex-col gap-2'>
        <h4 className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
          {t('Member runtime')}
        </h4>
        {runtime.members.length === 0 ? (
          <p className='text-muted-foreground text-sm'>
            {t('No lane members')}
          </p>
        ) : (
          <Table className='text-xs'>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Lane')}</TableHead>
                <TableHead>{t('Member')}</TableHead>
                <TableHead>{t('Available')}</TableHead>
                <TableHead>{t('Cooldown until')}</TableHead>
                <TableHead>{t('Circuit')}</TableHead>
                <TableHead>{t('Consecutive failures')}</TableHead>
                <TableHead>{t('Rolling success rate')}</TableHead>
                <TableHead>{t('Last error kind')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runtime.members.map((member) => {
                const cooldownUntil = isCooldownActive(
                  member.cooldown_until,
                  props.now
                )
                  ? formatStatusTime(member.cooldown_until)
                  : ''
                return (
                  <TableRow key={`${member.lane}/${member.member}`}>
                    <TableCell className='font-medium'>{member.lane}</TableCell>
                    <TableCell>
                      <span className='flex min-w-0 items-center gap-1.5'>
                        <span className='truncate font-mono'>
                          {member.member}
                        </span>
                        {!member.enabled && (
                          <StatusBadge
                            label={t('Manually disabled')}
                            variant='neutral'
                            size='sm'
                            copyable={false}
                          />
                        )}
                      </span>
                    </TableCell>
                    <TableCell>
                      <StatusBadge
                        label={
                          member.available ? t('Available') : t('Unavailable')
                        }
                        variant={member.available ? 'success' : 'danger'}
                        size='sm'
                        copyable={false}
                      />
                    </TableCell>
                    <TableCell className='whitespace-nowrap tabular-nums'>
                      {cooldownUntil || <EmptyCell />}
                    </TableCell>
                    <TableCell>
                      <StatusBadge
                        label={circuitLabel(t, member.circuit)}
                        variant={circuitVariant(member.circuit)}
                        size='sm'
                        copyable={false}
                      />
                    </TableCell>
                    <TableCell className='tabular-nums'>
                      {member.consecutive_failures}
                    </TableCell>
                    <TableCell className='tabular-nums'>
                      {formatSuccessRate(member.rolling_success_rate)}
                    </TableCell>
                    <TableCell>
                      {member.last_error_kind ? (
                        <span className='font-mono'>
                          {member.last_error_kind}
                        </span>
                      ) : (
                        <EmptyCell />
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}

        {upstreamRejected && (
          <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
            <AlertDescription>
              {t('Upstream rejected the request (not a channel fault).')}
            </AlertDescription>
          </Alert>
        )}
      </div>
    </SideDrawerSection>
  )
}
