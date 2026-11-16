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

import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'

import { attemptStatusClass, isSkipStatus } from '../attempt-status'
import type { PBRAttempt } from '../pbr-logs-api'

interface PBRAttemptTimelineProps {
  attempts: PBRAttempt[]
}

/**
 * attempts 逐尝试时间线（ui-spec §6.6）。
 *
 * 排障要看的是"为什么没用 P1、为什么最后 503"，所以把失败/冷却/熔断/跳过
 * 按顺序完整列出，并给跳过类状态保留原因说明。
 */
export function PBRAttemptTimeline(props: PBRAttemptTimelineProps) {
  const { t } = useTranslation()

  if (props.attempts.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>{t('No attempt records')}</p>
    )
  }

  return (
    <ScrollArea className='max-h-80 pr-2'>
      <ol className='flex flex-col gap-2'>
        {props.attempts.map((attempt) => (
          <li
            className='border-border/60 flex flex-col gap-1 rounded-md border p-2.5'
            key={`${attempt.attempt_num}-${attempt.member}-${attempt.status}`}
          >
            <div className='flex flex-wrap items-center gap-2'>
              <span className='text-muted-foreground text-xs tabular-nums'>
                #{attempt.attempt_num}
              </span>
              <span className='font-mono text-xs break-all'>
                {attempt.member}
              </span>
              <Badge
                className={cn(
                  'text-[11px]',
                  attemptStatusClass(attempt.status)
                )}
                variant='secondary'
              >
                {t(attempt.status)}
              </Badge>
              {attempt.duration_ms > 0 && (
                <span className='text-muted-foreground text-xs tabular-nums'>
                  {attempt.duration_ms} ms
                </span>
              )}
              {attempt.error_kind && (
                <span className='text-muted-foreground text-xs'>
                  {attempt.error_kind}
                </span>
              )}
            </div>
            {attempt.msg && (
              <p
                className={cn(
                  'text-xs break-words',
                  isSkipStatus(attempt.status)
                    ? 'text-muted-foreground'
                    : 'text-destructive'
                )}
              >
                {attempt.msg}
              </p>
            )}
          </li>
        ))}
      </ol>
    </ScrollArea>
  )
}
