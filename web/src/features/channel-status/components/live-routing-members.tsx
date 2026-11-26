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

import { cn } from '@/lib/utils'

import {
  remainingParts,
  type LiveMemberRow,
  type MemberLiveKind,
} from '../lib/live-routing'
import { ChannelAvatar } from './channel-avatar'

function statusTone(kind: MemberLiveKind): string {
  switch (kind) {
    case 'rate-limited':
    case 'circuit-open':
    case 'unavailable':
      return 'text-[var(--tone-destructive-text)]'
    case 'cooldown':
    case 'half-open':
    case 'rejected':
      return 'text-[var(--tone-warning-text)]'
    case 'probing':
      return 'text-[var(--tone-info-text)]'
    case 'current':
      return 'text-foreground font-medium'
    case 'waiting':
      return 'text-muted-foreground/70'
    default:
      return 'text-muted-foreground'
  }
}

export interface MemberRowsProps {
  rows: LiveMemberRow[]
  /** 当前车道强调色（用于选中排位与当前成员描边）。 */
  accent: string
  registerRef: (key: string, el: HTMLDivElement | null) => void
}

/**
 * 实时路由右栏：按路由顺序列出成员，右侧是运行态结论。
 *
 * 状态文案与颜色只表达运行态事实：`available=false` 不等于故障，所以
 * 「冷却」「熔断」「探活」「人工停用」「渠道停用」必须分开写，不能合并成
 * 一个“异常”。倒计时由上游注入的 now 驱动（见 useLaneRuntime.nowMs）。
 */
export function MemberRows(props: MemberRowsProps) {
  const { t } = useTranslation()

  const remaining = (seconds: number): string => {
    const parts = remainingParts(seconds)
    if (parts.hours > 0) {
      return t('{{hours}}h {{minutes}}m', {
        hours: parts.hours,
        minutes: parts.minutes,
      })
    }
    if (parts.minutes > 0) {
      return t('{{minutes}}m {{seconds}}s', {
        minutes: parts.minutes,
        seconds: parts.seconds,
      })
    }
    return t('{{value}}s', { value: parts.seconds })
  }

  const statusText = (row: LiveMemberRow): string => {
    const state = row.state
    const time = state.secondsLeft !== null ? remaining(state.secondsLeft) : ''
    switch (state.kind) {
      case 'lane-disabled':
        return t('Lane disabled')
      case 'disabled':
        return t('Manually disabled')
      case 'channel-disabled':
        return t('Channel disabled')
      case 'circuit-open':
        return state.secondsLeft !== null
          ? t('Circuit open · {{time}} left', { time })
          : t('Circuit open')
      case 'rate-limited':
        return t('Rate limited · {{time}} left', { time })
      case 'cooldown':
        return t('Cooling down · {{time}} left', { time })
      case 'probing':
        return t('Probing…')
      case 'half-open':
        return t('Half-open · waiting for recovery')
      case 'rejected':
        return t('Upstream rejected · {{count}} in a row', {
          count: state.streak ?? 0,
        })
      case 'current':
        return t('Current member')
      case 'ready':
        return t('Ready')
      case 'waiting':
        return t('Waiting for runtime data')
      default:
        return t('Unavailable')
    }
  }

  if (props.rows.length === 0) {
    return (
      <p className='text-muted-foreground rounded-lg border border-dashed px-3 py-6 text-center text-xs'>
        {t('No members in this lane')}
      </p>
    )
  }

  return (
    <div className='flex flex-col gap-2' role='list'>
      {props.rows.map((row, index) => {
        const current = row.state.current
        const muted =
          row.state.kind === 'disabled' ||
          row.state.kind === 'channel-disabled' ||
          row.state.kind === 'lane-disabled'
        return (
          <div
            key={row.key}
            role='listitem'
            ref={(el) => props.registerRef(row.key, el)}
            data-current={current || undefined}
            className={cn(
              'rounded-lg border px-3 py-2.5 transition-colors',
              current
                ? 'border-foreground/25 bg-card shadow-sm ring-1 ring-foreground/10'
                : 'border-border/60 bg-card/60 hover:border-foreground/15 hover:bg-card',
              muted && 'opacity-65'
            )}
          >
            <div className='flex min-w-0 items-center gap-2.5'>
              <span
                aria-hidden='true'
                className='border-border/70 text-muted-foreground flex size-6 shrink-0 items-center justify-center rounded-full border text-[11px] tabular-nums'
                style={
                  current
                    ? { borderColor: props.accent, color: props.accent }
                    : undefined
                }
              >
                {index + 1}
              </span>
              <ChannelAvatar name={row.channel} />
              <span className='min-w-0 flex-1'>
                <span className='block truncate text-sm font-medium'>
                  {row.channel}
                </span>
                <span
                  className='text-muted-foreground block truncate font-mono text-[11px]'
                  title={row.upstreamModel}
                >
                  {row.group
                    ? `${t('Member group')} · ${row.groupMemberCount ?? 0} ${t('Members')}`
                    : row.upstreamModel || '-'}
                </span>
              </span>
              <span
                className={cn(
                  'shrink-0 text-right text-xs whitespace-nowrap',
                  statusTone(row.state.kind)
                )}
                title={
                  row.state.kind === 'rejected'
                    ? t('Upstream rejected the request (not a channel fault).')
                    : undefined
                }
              >
                {statusText(row)}
              </span>
            </div>
          </div>
        )
      })}
    </div>
  )
}
