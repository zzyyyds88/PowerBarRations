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
import dayjs from 'dayjs'
import { KeyRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  formatDurationMs,
  formatSuccessRate,
} from '@/features/channels/lib/channel-status-display'
import {
  attemptStatusClass,
  isSkipStatus,
} from '@/features/usage-logs/pbr/attempt-status'
import type {
  PBRAttempt,
  PBRRequestLogListItem,
} from '@/features/usage-logs/pbr/pbr-logs-api'
import { formatNumber } from '@/lib/format'
import type { LaneMemberHealth } from '@/lib/route-events'
import { cn } from '@/lib/utils'

import { memberLabel, type LaneWindowStats } from '../lib/live-routing'

function AttemptChip(props: { attempt: PBRAttempt }) {
  const { t } = useTranslation()
  const channel = props.attempt.member.split('/')[0]
  return (
    <span
      className='border-border/60 bg-muted/30 inline-flex min-w-0 items-center gap-1 rounded-md border px-1.5 py-0.5'
      title={props.attempt.member}
    >
      <span className='text-muted-foreground text-[10px] tabular-nums'>
        #{props.attempt.attempt_num}
      </span>
      <span className='max-w-[7rem] truncate text-[11px]'>{channel}</span>
      <span
        className={cn(
          'rounded px-1 text-[10px] font-medium',
          attemptStatusClass(props.attempt.status)
        )}
      >
        {t(props.attempt.status)}
      </span>
      {props.attempt.error_kind && !isSkipStatus(props.attempt.status) && (
        <span className='text-muted-foreground text-[10px]'>
          {props.attempt.error_kind}
        </span>
      )}
    </span>
  )
}

export interface LiveRoutingFooterProps {
  laneEnabled: boolean
  firstMember: LaneMemberHealth | null
  stats: LaneWindowStats
  log: PBRRequestLogListItem | undefined
  logPending: boolean
}

/** 实时路由卡底部：首选结论、车道窗口计数、最近一次请求的尝试链。 */
export function LiveRoutingFooter(props: LiveRoutingFooterProps) {
  const { t, i18n } = useTranslation()
  const log = props.log

  return (
    <div className='border-border/60 flex flex-col gap-3 border-t pt-3'>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
        <p className='min-w-0 text-xs'>
          {props.laneEnabled && props.firstMember ? (
            <span className='inline-flex min-w-0 items-center gap-1.5'>
              <span className='bg-success size-1.5 shrink-0 rounded-full' />
              <span className='text-muted-foreground shrink-0'>
                {t('First available')}:
              </span>
              <span
                className='truncate font-medium'
                title={memberLabel(props.firstMember)}
              >
                {memberLabel(props.firstMember)}
              </span>
            </span>
          ) : (
            <span className='text-muted-foreground'>
              {props.laneEnabled
                ? t('No member is currently available')
                : t('Lane disabled')}
            </span>
          )}
        </p>
        <div className='text-muted-foreground flex shrink-0 items-center gap-2 text-[11px] tabular-nums'>
          <span>
            <span className='text-foreground font-medium'>
              {formatNumber(props.stats.requests, i18n.language)}
            </span>{' '}
            {t('Requests')}
          </span>
          <span aria-hidden='true' className='text-muted-foreground/40'>
            ·
          </span>
          <span>
            <span className='text-foreground font-medium'>
              {formatNumber(props.stats.failures, i18n.language)}
            </span>{' '}
            {t('Failures')}
          </span>
          <span aria-hidden='true' className='text-muted-foreground/40'>
            ·
          </span>
          <span>
            <span className='text-foreground font-medium'>
              {formatSuccessRate(props.stats.successRate)}
            </span>{' '}
            {t('Success rate')}
          </span>
        </div>
      </div>

      <div className='bg-muted/25 rounded-lg px-3 py-2'>
        <div className='flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px]'>
          <span className='text-muted-foreground'>{t('Recent request')}</span>
          {props.logPending && (
            <span className='text-muted-foreground'>{t('Loading...')}</span>
          )}
          {!props.logPending && !log && (
            <span className='text-muted-foreground'>
              {t('No requests recorded yet')}
            </span>
          )}
          {log && (
            <>
              <span className='text-muted-foreground tabular-nums'>
                {dayjs(log.ts).format('MM-DD HH:mm:ss')}
              </span>
              {log.key_name && (
                <span className='text-muted-foreground border-border/60 bg-card inline-flex items-center gap-1 rounded border px-1.5 py-0.5'>
                  <KeyRound className='size-3' aria-hidden='true' />
                  <span className='max-w-[8rem] truncate'>{log.key_name}</span>
                </span>
              )}
              <span className='min-w-0 truncate'>
                <span className='text-muted-foreground'>{log.lane}</span>
                <span className='text-muted-foreground mx-1'>→</span>
                <span className='font-mono'>
                  {log.channel || '-'}
                  {log.upstream_model ? `/${log.upstream_model}` : ''}
                </span>
              </span>
              <span
                className={cn(
                  'rounded px-1 text-[10px] font-medium',
                  log.success
                    ? 'bg-success/15 text-[var(--tone-success-text)]'
                    : 'bg-destructive/15 text-[var(--tone-destructive-text)]'
                )}
              >
                {log.success ? t('success') : t('failed')}
              </span>
              <span className='text-muted-foreground tabular-nums'>
                {log.http_status > 0 ? `${log.http_status} · ` : ''}
                {formatDurationMs(log.total_ms)}
              </span>
            </>
          )}
        </div>
        {log && log.attempts.length > 0 && (
          <div className='mt-2 flex flex-wrap items-center gap-1.5'>
            {log.attempts.map((attempt) => (
              <AttemptChip
                key={`${attempt.attempt_num}-${attempt.member}-${attempt.status}`}
                attempt={attempt}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
