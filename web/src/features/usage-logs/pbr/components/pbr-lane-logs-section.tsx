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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cn } from '@/lib/utils'

import { summarizeAttempts } from '../attempt-status'
import {
  getPBRRequestLog,
  listPBRRequestLogs,
  type PBRLogFilters,
} from '../pbr-logs-api'
import { PBRAttemptTimeline } from './pbr-attempt-timeline'

const PAGE_SIZE = 50

/**
 * PBR 元数据日志分节（ui-spec §6.6）。
 *
 * 数据源是 PBR 自己的 `/api/v1/logs`：基座 `/api/log/**` 没有车道/成员/attempts 链，
 * 排障时看不到"为什么没用 P1、为什么最后 503"。明细可被保留策略清理，
 * 历史聚合请看数据看板（读小时聚合表）。
 */
export function PBRLaneLogsSection() {
  const { t } = useTranslation()
  const [filters, setFilters] = useState<PBRLogFilters>({})
  const [successFilter, setSuccessFilter] = useState<'all' | 'true' | 'false'>(
    'all'
  )
  const [cursors, setCursors] = useState<string[]>([])
  const [cursor, setCursor] = useState<string | undefined>(undefined)
  const [detailId, setDetailId] = useState<number | null>(null)

  const logsQuery = useQuery({
    queryKey: ['pbr-request-logs', filters, successFilter, cursor],
    queryFn: () =>
      listPBRRequestLogs({
        ...filters,
        limit: PAGE_SIZE,
        cursor,
        ...(successFilter === 'all'
          ? {}
          : { success: successFilter === 'true' }),
      }),
  })

  const detailQuery = useQuery({
    queryKey: ['pbr-request-log', detailId],
    enabled: detailId !== null,
    queryFn: () => getPBRRequestLog(detailId as number),
  })

  const applyFilter = (patch: PBRLogFilters) => {
    setCursors([])
    setCursor(undefined)
    setFilters((prev) => ({ ...prev, ...patch }))
  }

  if (logsQuery.isPending) {
    return <LoadingState />
  }
  if (logsQuery.isError) {
    return (
      <ErrorState
        description={t('Failed to load request logs')}
        title={t('Request logs')}
      />
    )
  }

  const items = logsQuery.data.items

  return (
    <div className='flex h-full min-h-0 flex-col gap-3'>
      <div className='flex flex-wrap items-center gap-2'>
        <Input
          className='h-8 w-40'
          onChange={(event) => applyFilter({ lane: event.target.value })}
          placeholder={t('Lane')}
          value={filters.lane ?? ''}
        />
        <Input
          className='h-8 w-40'
          onChange={(event) => applyFilter({ channel: event.target.value })}
          placeholder={t('Channel')}
          value={filters.channel ?? ''}
        />
        <Input
          className='h-8 w-40'
          onChange={(event) => applyFilter({ key: event.target.value })}
          placeholder={t('Key')}
          value={filters.key ?? ''}
        />
        <Input
          className='h-8 w-40'
          onChange={(event) => applyFilter({ model: event.target.value })}
          placeholder={t('Request model')}
          value={filters.model ?? ''}
        />
        <Select
          onValueChange={(value) => {
            setCursors([])
            setCursor(undefined)
            setSuccessFilter(value as 'all' | 'true' | 'false')
          }}
          value={successFilter}
        >
          <SelectTrigger className='h-8 w-32'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='all'>{t('All')}</SelectItem>
            <SelectItem value='true'>{t('Success')}</SelectItem>
            <SelectItem value='false'>{t('Failed')}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {items.length === 0 ? (
        <EmptyState
          description={t(
            'No request metadata yet. Metadata logs keep the lane, member chain and token usage, but never request or response bodies.'
          )}
          title={t('No request logs')}
        />
      ) : (
        <div className='min-h-0 flex-1 overflow-auto rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Time')}</TableHead>
                <TableHead>{t('Lane')}</TableHead>
                <TableHead>{t('Channel')}</TableHead>
                <TableHead>{t('Upstream model')}</TableHead>
                <TableHead>{t('Key')}</TableHead>
                <TableHead>{t('Result')}</TableHead>
                <TableHead>{t('Attempts')}</TableHead>
                <TableHead className='text-right'>{t('Total ms')}</TableHead>
                <TableHead className='text-right'>
                  {t('Cost (converted)')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow
                  className='cursor-pointer'
                  key={item.id}
                  onClick={() => setDetailId(item.id)}
                >
                  <TableCell className='text-xs whitespace-nowrap'>
                    {item.ts}
                  </TableCell>
                  <TableCell className='text-xs'>{item.lane}</TableCell>
                  <TableCell className='text-xs'>{item.channel}</TableCell>
                  <TableCell className='font-mono text-xs'>
                    {item.upstream_model}
                  </TableCell>
                  <TableCell className='text-xs'>{item.key_name}</TableCell>
                  <TableCell>
                    <Badge
                      className={cn(
                        'text-[11px]',
                        item.success
                          ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300'
                          : 'bg-destructive/15 text-destructive'
                      )}
                      variant='secondary'
                    >
                      {item.success
                        ? t('Success')
                        : item.error_kind || t('Failed')}
                    </Badge>
                  </TableCell>
                  <TableCell className='font-mono text-[11px] break-all'>
                    {summarizeAttempts(item.attempts)}
                  </TableCell>
                  <TableCell className='text-right text-xs tabular-nums'>
                    {item.total_ms}
                  </TableCell>
                  <TableCell className='text-right text-xs tabular-nums'>
                    {item.estimated_cost.toFixed(4)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <div className='flex items-center justify-end gap-2'>
        <Button
          disabled={cursors.length === 0}
          onClick={() => {
            const previous = [...cursors]
            previous.pop()
            setCursors(previous)
            setCursor(previous.at(-1))
          }}
          size='sm'
          variant='outline'
        >
          {t('Previous')}
        </Button>
        <Button
          disabled={!logsQuery.data.next_cursor}
          onClick={() => {
            const next = logsQuery.data.next_cursor
            if (!next) return
            setCursors((prev) => [...prev, next])
            setCursor(next)
          }}
          size='sm'
          variant='outline'
        >
          {t('Next')}
        </Button>
      </div>

      <Dialog
        size='lg'
        onOpenChange={(open) => {
          if (!open) setDetailId(null)
        }}
        open={detailId !== null}
        title={t('Request detail')}
        description={t(
          'Attempt chain: which member served the request and why the others were skipped.'
        )}
      >
        {detailQuery.isPending && <LoadingState />}
        {detailQuery.data && (
          <div className='flex flex-col gap-3'>
            <dl className='grid grid-cols-2 gap-x-4 gap-y-1 text-xs sm:grid-cols-3'>
              <DetailItem label={t('Lane')} value={detailQuery.data.lane} />
              <DetailItem
                label={t('Request model')}
                value={detailQuery.data.request_model}
              />
              <DetailItem
                label={t('Upstream model')}
                value={detailQuery.data.upstream_model}
              />
              <DetailItem
                label={t('Channel')}
                value={detailQuery.data.channel}
              />
              <DetailItem
                label={t('Inbound format')}
                value={detailQuery.data.inbound_format}
              />
              <DetailItem
                label={t('HTTP status')}
                value={String(detailQuery.data.http_status)}
              />
              <DetailItem
                label={t('Prompt tokens')}
                value={String(detailQuery.data.prompt_tokens)}
              />
              <DetailItem
                label={t('Completion tokens')}
                value={String(detailQuery.data.completion_tokens)}
              />
              <DetailItem
                label={t('Total ms')}
                value={String(detailQuery.data.total_ms)}
              />
            </dl>
            {detailQuery.data.error_summary && (
              <p className='text-destructive text-xs break-words'>
                {detailQuery.data.error_summary}
              </p>
            )}
            <PBRAttemptTimeline attempts={detailQuery.data.attempts ?? []} />
          </div>
        )}
      </Dialog>
    </div>
  )
}

function DetailItem(props: { label: string; value: string }) {
  return (
    <div className='flex flex-col'>
      <dt className='text-muted-foreground'>{props.label}</dt>
      <dd className='font-mono break-all'>{props.value || '-'}</dd>
    </div>
  )
}
