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
import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { api } from '@/lib/api'

import { SettingsSection } from '../components/settings-section'

interface DeliveryEntry {
  id: number
  ts: string | number
  target: string
  event_type: string
  lane: string
  member: string
  status: string
  http_status: number
  error: string
  attempt: number
}

interface DeliveriesResponse {
  items: DeliveryEntry[]
  dropped_events?: number
}

const formatTime = (ts: string | number): string => {
  if (typeof ts === 'number') {
    return new Date(ts * 1000).toLocaleString()
  }
  return new Date(ts).toLocaleString()
}

export function WebhookDeliveriesSection() {
  const { t } = useTranslation()
  const deliveries = useQuery({
    queryKey: ['pbr-webhook-deliveries'],
    queryFn: async () => {
      const res = await api.get<DeliveriesResponse>(
        '/api/v1/webhooks/deliveries',
        { params: { limit: 20 } }
      )
      return res.data
    },
    refetchInterval: 30_000,
  })

  const items = deliveries.data?.items ?? []
  const dropped = deliveries.data?.dropped_events ?? 0

  return (
    <SettingsSection title={t('Alert Deliveries')}>
      <div className='flex items-start justify-between gap-3'>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Recent webhook deliveries, including dead letters. Failed rows show the receiver error and the final attempt count.'
          )}
          {dropped > 0
            ? ` ${t('Dropped events: {{count}}', { count: dropped })}`
            : ''}
        </p>
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={deliveries.isFetching}
          onClick={() => deliveries.refetch()}
        >
          <RefreshCw className='size-3.5' />
          {t('Refresh')}
        </Button>
      </div>

      {items.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('No deliveries yet.')}
        </p>
      ) : (
        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Time')}</TableHead>
                <TableHead>{t('Event')}</TableHead>
                <TableHead>{t('Lane / Member')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>{t('HTTP')}</TableHead>
                <TableHead>{t('Attempts')}</TableHead>
                <TableHead>{t('Error')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((entry) => (
                <TableRow key={entry.id}>
                  <TableCell className='text-xs whitespace-nowrap'>
                    {formatTime(entry.ts)}
                  </TableCell>
                  <TableCell className='font-mono text-xs'>
                    {entry.event_type}
                  </TableCell>
                  <TableCell className='font-mono text-xs'>
                    {[entry.lane, entry.member].filter(Boolean).join(' / ') ||
                      '-'}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        entry.status === 'success' ? 'secondary' : 'destructive'
                      }
                    >
                      {entry.status}
                    </Badge>
                  </TableCell>
                  <TableCell className='text-xs'>
                    {entry.http_status || '-'}
                  </TableCell>
                  <TableCell className='text-xs'>{entry.attempt}</TableCell>
                  <TableCell className='max-w-[28rem] truncate text-xs'>
                    <span title={entry.error || undefined}>
                      {entry.error || '-'}
                    </span>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </SettingsSection>
  )
}
