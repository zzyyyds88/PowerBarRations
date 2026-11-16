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
import { ExternalLink } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { CONSOLE_ROUTES } from '@/config/routes'
import { formatTimestampToDate } from '@/lib/format'

import type { PrivacyRecord } from '../privacy-api'

export function PrivacyRecordDialog(props: {
  record: PrivacyRecord | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const record = props.record
  return (
    <Dialog
      open={Boolean(record)}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Masking details')}
      description={
        record
          ? `${record.request_model} · ${formatTimestampToDate(Math.floor(Date.parse(record.ts) / 1000))}`
          : undefined
      }
      size='lg'
    >
      {record && (
        <div className='space-y-4'>
          <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
            <div className='rounded-md border p-2'>
              <div className='text-muted-foreground text-xs'>{t('Masked')}</div>
              <div className='font-mono text-sm'>
                {record.masked_count ?? 0}
              </div>
            </div>
            <div className='rounded-md border p-2'>
              <div className='text-muted-foreground text-xs'>{t('Status')}</div>
              <div className='text-sm'>{record.mask_status || '—'}</div>
            </div>
            <div className='rounded-md border p-2'>
              <div className='text-muted-foreground text-xs'>{t('Took')}</div>
              <div className='font-mono text-sm'>
                {record.mask_took_ms ?? 0} ms
              </div>
            </div>
            <div className='rounded-md border p-2'>
              <div className='text-muted-foreground text-xs'>{t('Client')}</div>
              <div className='truncate text-sm'>
                {record.client_name || record.client || '—'}
              </div>
            </div>
          </div>

          <div className='overflow-hidden rounded-md border'>
            <table className='w-full text-xs'>
              <thead className='bg-muted/40 text-muted-foreground'>
                <tr>
                  <th className='px-2 py-1.5 text-left'>{t('Kind')}</th>
                  <th className='px-2 py-1.5 text-left'>{t('Found by')}</th>
                  <th className='px-2 py-1.5 text-left'>{t('Placeholder')}</th>
                  <th className='px-2 py-1.5 text-right'>{t('Hint')}</th>
                  <th className='px-2 py-1.5 text-right'>{t('Count')}</th>
                </tr>
              </thead>
              <tbody className='divide-y'>
                {record.mask_hits.map((hit) => (
                  <tr key={`${hit.kind}-${hit.placeholder}`}>
                    <td className='px-2 py-1.5 font-mono'>{hit.kind}</td>
                    <td className='px-2 py-1.5'>{hit.by}</td>
                    <td className='px-2 py-1.5 font-mono'>{hit.placeholder}</td>
                    <td className='text-muted-foreground px-2 py-1.5 text-right'>
                      {hit.head ? `${hit.head}…` : '—'} · {hit.len}
                    </td>
                    <td className='px-2 py-1.5 text-right font-mono'>
                      {hit.n}
                      {hit.action ? ` · ${hit.action}` : ''}
                    </td>
                  </tr>
                ))}
                {record.mask_hits.length === 0 && (
                  <tr>
                    <td
                      colSpan={5}
                      className='text-muted-foreground px-2 py-6 text-center'
                    >
                      {t('No values were masked.')}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          <div className='flex items-center justify-between gap-2'>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Only kinds, placeholders and hints are stored; the values and prompt are never kept.'
              )}
            </p>
            <a
              href={`${CONSOLE_ROUTES.logsRequests}?session=${encodeURIComponent(record.session_key ?? '')}`}
              className='text-primary inline-flex items-center gap-1 text-xs underline-offset-4 hover:underline'
            >
              {t('Open request log')}
              <ExternalLink className='size-3' aria-hidden='true' />
            </a>
          </div>
        </div>
      )}
    </Dialog>
  )
}
