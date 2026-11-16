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
import { ExternalLink } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { formatCostYuan } from '@/features/keys/lib/cost'
import { formatTimestampToDate, formatTokens } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { PBRSession } from '../../pbr/pbr-sessions-api'

function contextTone(ratio: number): string {
  if (ratio >= 0.9) return 'bg-rose-500'
  if (ratio >= 0.75) return 'bg-amber-500'
  return 'bg-emerald-500'
}

function groupLabel(group: string, t: (key: string) => string): string {
  return group === 'script' ? t('Scripts') : group
}

export function useSessionColumns(props: {
  onOpen: (session: PBRSession) => void
}): ColumnDef<PBRSession>[] {
  const { t } = useTranslation()
  return [
    {
      id: 'session',
      header: t('Session'),
      accessorFn: (row) => row.session_id,
      cell: ({ row }) => {
        const session = row.original
        const groups = session.client_groups ?? []
        return (
          <button
            type='button'
            onClick={() => props.onOpen(session)}
            className='flex min-w-0 flex-col items-start gap-0.5 text-left'
          >
            <span className='max-w-[18rem] truncate font-mono text-xs'>
              {session.session_id || session.session_key}
            </span>
            <span className='text-muted-foreground flex flex-wrap gap-1 text-xs'>
              {groups.length > 0
                ? groups.map((group) => groupLabel(group, t)).join(' · ')
                : t('Unknown client')}
            </span>
          </button>
        )
      },
      meta: { contentSized: true },
      size: 260,
    },
    {
      id: 'last_ts',
      header: t('Last activity'),
      accessorFn: (row) => row.last_ts,
      cell: ({ row }) => (
        <span className='font-mono text-xs tabular-nums'>
          {formatTimestampToDate(
            Math.floor(Date.parse(row.original.last_ts) / 1000)
          )}
        </span>
      ),
      meta: { contentSized: true },
      size: 160,
    },
    {
      id: 'requests',
      header: t('Requests'),
      accessorFn: (row) => row.requests,
      cell: ({ row }) => (
        <span className='font-mono text-xs tabular-nums'>
          {row.original.requests}
          {row.original.errors > 0 && (
            <span className='text-destructive ml-1'>
              ({row.original.errors})
            </span>
          )}
        </span>
      ),
      meta: { contentSized: true },
      size: 90,
    },
    {
      id: 'tokens',
      header: t('Tokens'),
      accessorFn: (row) => row.prompt_tokens + row.completion_tokens,
      cell: ({ row }) => {
        const session = row.original
        const total = session.prompt_tokens + session.completion_tokens
        return (
          <span
            className='font-mono text-xs tabular-nums'
            title={`${t('Input')} ${formatTokens(session.prompt_tokens)} · ${t('Output')} ${formatTokens(session.completion_tokens)} · ${t('Cache read')} ${formatTokens(session.cache_read_tokens)}`}
          >
            {formatTokens(total)}
          </span>
        )
      },
      meta: { contentSized: true },
      size: 90,
    },
    {
      id: 'estimated_cost',
      header: t('Cost'),
      accessorFn: (row) => row.estimated_cost,
      cell: ({ row }) => (
        <span className='font-mono text-xs tabular-nums'>
          {row.original.estimated_cost > 0
            ? formatCostYuan(row.original.estimated_cost)
            : '—'}
        </span>
      ),
      meta: { contentSized: true },
      size: 100,
    },
    {
      id: 'models',
      header: t('Models'),
      accessorFn: (row) => row.models,
      cell: ({ row }) => {
        const models = row.original.models ?? []
        if (models.length === 0) {
          return <span className='text-muted-foreground'>—</span>
        }
        const shown = models.slice(0, 3)
        return (
          <span
            className='flex max-w-56 flex-wrap gap-1'
            title={models.join(', ')}
          >
            {shown.map((model) => (
              <span
                key={model}
                className='bg-muted inline-flex max-w-28 truncate rounded px-1.5 py-0.5 font-mono text-xs'
              >
                {model}
              </span>
            ))}
            {models.length > shown.length && (
              <span className='text-muted-foreground text-xs'>
                +{models.length - shown.length}
              </span>
            )}
          </span>
        )
      },
      meta: { contentSized: true },
      size: 200,
    },
    {
      id: 'context',
      header: t('Context'),
      accessorFn: (row) => row.context_peak_tokens,
      cell: ({ row }) => {
        const session = row.original
        if (session.context_peak_tokens <= 0) {
          return <span className='text-muted-foreground'>—</span>
        }
        if (session.context_window > 0) {
          const ratio = Math.min(
            1,
            session.context_peak_tokens / session.context_window
          )
          return (
            <span className='flex min-w-24 items-center gap-1.5'>
              <span className='bg-muted h-1.5 w-16 overflow-hidden rounded-full'>
                <span
                  className={cn(
                    'block h-full rounded-full',
                    contextTone(ratio)
                  )}
                  style={{ width: `${Math.max(2, ratio * 100)}%` }}
                />
              </span>
              <span className='font-mono text-xs tabular-nums'>
                {Math.round(ratio * 100)}%
              </span>
            </span>
          )
        }
        return (
          <span className='font-mono text-xs tabular-nums'>
            {formatTokens(session.context_peak_tokens)}
          </span>
        )
      },
      meta: { contentSized: true },
      size: 140,
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <button
          type='button'
          onClick={() => props.onOpen(row.original)}
          className='text-muted-foreground hover:text-foreground inline-flex size-7 items-center justify-center rounded-md transition-colors'
          aria-label={t('Open session')}
        >
          <ExternalLink className='size-3.5' aria-hidden='true' />
        </button>
      ),
      enableSorting: false,
      meta: { contentSized: true },
      size: 48,
    },
  ]
}
