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
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { CONSOLE_ROUTES } from '@/config/routes'
import { formatTokens } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { UsageLog } from '../../data/schema'
import { parseLogOther } from '../../lib/format'

function contextFillTone(ratio: number): string {
  if (ratio >= 0.9) return 'text-rose-600'
  if (ratio >= 0.75) return 'text-amber-600'
  return 'text-emerald-600'
}

/**
 * PBR-only usage columns (ui-spec §6 日志): the client that made the call,
 * the gateway session it belongs to, and how full its context was.
 */
export function usePBRUsageColumns(): ColumnDef<UsageLog>[] {
  const { t } = useTranslation()
  return [
    {
      id: 'client',
      header: t('Client'),
      accessorFn: (row) => {
        const pbr = parseLogOther(row.other)?.pbr
        return pbr?.client_name || pbr?.client || ''
      },
      cell: ({ row }) => {
        const pbr = parseLogOther(row.original.other)?.pbr
        if (!pbr?.client) {
          return <span className='text-muted-foreground'>—</span>
        }
        const isScript = pbr.client_group === 'script'
        return (
          <span
            className='inline-flex max-w-36 items-center truncate text-xs'
            title={`${pbr.client_name || pbr.client} (${pbr.client})`}
          >
            {isScript ? t('Scripts') : pbr.client_name || pbr.client}
          </span>
        )
      },
      meta: { contentSized: true },
      size: 140,
    },
    {
      id: 'session',
      header: t('Session'),
      accessorFn: (row) => parseLogOther(row.other)?.pbr?.session_id ?? '',
      cell: ({ row }) => {
        const pbr = parseLogOther(row.original.other)?.pbr
        if (!pbr?.session_id) {
          return <span className='text-muted-foreground'>—</span>
        }
        const key = pbr.session_key || pbr.session_id
        return (
          <a
            href={`${CONSOLE_ROUTES.logsSessions}?session=${encodeURIComponent(key)}`}
            className='text-primary inline-block max-w-40 truncate font-mono text-xs underline-offset-4 hover:underline'
            title={pbr.session_id}
          >
            {pbr.session_id}
          </a>
        )
      },
      meta: { contentSized: true },
      size: 160,
    },
    {
      id: 'masked',
      header: t('Masked'),
      accessorFn: (row) => parseLogOther(row.other)?.pbr?.masked_count ?? 0,
      cell: ({ row }) => {
        const pbr = parseLogOther(row.original.other)?.pbr
        if (!pbr?.masked_count) {
          return <span className='text-muted-foreground'>—</span>
        }
        if (pbr.mask_blocked) {
          return (
            <span className='text-xs font-medium text-rose-600'>
              {t('Blocked')}
            </span>
          )
        }
        return (
          <span className='font-mono text-xs tabular-nums'>
            {pbr.masked_count}
          </span>
        )
      },
      meta: { contentSized: true },
      size: 90,
    },
    {
      id: 'context',
      header: t('Context'),
      accessorFn: (row) => parseLogOther(row.other)?.pbr?.context_tokens ?? 0,
      cell: ({ row }) => {
        const pbr = parseLogOther(row.original.other)?.pbr
        const tokens = pbr?.context_tokens ?? 0
        const window = pbr?.context_window ?? 0
        if (!pbr || pbr.context_status !== 'ok' || tokens <= 0) {
          return (
            <span
              className='text-muted-foreground text-xs'
              title={
                pbr?.context_status
                  ? t('Context is not available for this request')
                  : undefined
              }
            >
              —
            </span>
          )
        }
        if (window > 0) {
          const ratio = Math.min(1, tokens / window)
          return (
            <span
              className={cn(
                'font-mono text-xs tabular-nums',
                contextFillTone(ratio)
              )}
              title={`${formatTokens(tokens)} / ${formatTokens(window)}`}
            >
              {Math.round(ratio * 100)}%
            </span>
          )
        }
        return (
          <span className='font-mono text-xs tabular-nums'>
            {formatTokens(tokens)}
          </span>
        )
      },
      meta: { contentSized: true },
      size: 90,
    },
  ]
}
