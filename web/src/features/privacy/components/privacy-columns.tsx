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

import { Badge } from '@/components/ui/badge'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { PrivacyRecord } from '../privacy-api'

function statusLabel(status: string | undefined, t: (key: string) => string) {
  switch (status) {
    case 'model_unavailable':
      return t('Model unavailable')
    case 'partial':
      return t('Partial')
    case 'truncated':
      return t('Truncated')
    case 'unsupported':
      return t('Not inspected')
    case 'restore_failed':
      return t('Restore failed')
    default:
      return t('Masked')
  }
}

export function usePrivacyColumns(props: {
  onOpen: (record: PrivacyRecord) => void
}): ColumnDef<PrivacyRecord>[] {
  const { t } = useTranslation()
  return [
    {
      id: 'ts',
      header: t('Time'),
      accessorFn: (row) => row.ts,
      cell: ({ row }) =>
        formatTimestampToDate(Math.floor(Date.parse(row.original.ts) / 1000)),
      size: 150,
    },
    {
      id: 'client',
      header: t('Client'),
      accessorFn: (row) => row.client_name || row.client || '',
      cell: ({ row }) => row.original.client_name || row.original.client || '—',
    },
    {
      id: 'model',
      header: t('Request model'),
      accessorFn: (row) => row.request_model,
      cell: ({ row }) => (
        <span className='font-mono text-xs'>{row.original.request_model}</span>
      ),
    },
    {
      id: 'masked',
      header: t('Masked'),
      accessorFn: (row) => row.masked_count ?? 0,
      cell: ({ row }) => (
        <span className='font-mono text-xs tabular-nums'>
          {row.original.masked_count ?? 0}
        </span>
      ),
      size: 90,
    },
    {
      id: 'status',
      header: t('Status'),
      accessorFn: (row) => row.mask_status ?? '',
      cell: ({ row }) => (
        <Badge
          variant='outline'
          className={cn(
            'text-xs',
            row.original.mask_status === 'model_unavailable' &&
              'border-amber-500/40 text-amber-700 dark:text-amber-400',
            row.original.mask_blocked &&
              'border-rose-500/40 text-rose-700 dark:text-rose-400'
          )}
        >
          {row.original.mask_blocked
            ? t('Blocked')
            : statusLabel(row.original.mask_status, t)}
        </Badge>
      ),
      size: 130,
    },
    {
      id: 'session',
      header: t('Session'),
      accessorFn: (row) => row.session_id ?? '',
      cell: ({ row }) =>
        row.original.session_id ? (
          <span className='font-mono text-xs'>
            {row.original.session_id.slice(0, 12)}
          </span>
        ) : (
          '—'
        ),
      size: 120,
    },
    {
      id: 'open',
      header: '',
      cell: ({ row }) => (
        <button
          type='button'
          className='text-primary text-xs underline-offset-4 hover:underline'
          onClick={() => props.onOpen(row.original)}
        >
          {t('Details')}
        </button>
      ),
      enableSorting: false,
      size: 70,
    },
  ]
}
