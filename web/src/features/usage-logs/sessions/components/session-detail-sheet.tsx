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
import { ChevronLeft, ChevronRight, ExternalLink } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { CONSOLE_ROUTES } from '@/config/routes'
import { formatCostYuan } from '@/features/keys/lib/cost'
import { formatTimestampToDate, formatTokens } from '@/lib/format'
import { cn } from '@/lib/utils'

import { PBRContextCard } from '../../components/pbr-context-card'
import { PBRLogDetailsContent } from '../../components/pbr-log-details'
import { mapPBRLogToUsageLog } from '../../lib/pbr-mapper'
import type { PBRContext, PBRRequestLogListItem } from '../../pbr/pbr-logs-api'
import {
  getPBRSession,
  type PBRSessionDetail,
  type PBRSessionRequest,
} from '../../pbr/pbr-sessions-api'
import { ContextSparkline } from './context-sparkline'

function Kpi(props: { label: string; value: string; tone?: string }) {
  return (
    <div className='bg-muted/20 min-w-0 rounded-md border px-2.5 py-2'>
      <div className='text-muted-foreground truncate text-xs'>
        {props.label}
      </div>
      <div
        className={cn(
          'mt-0.5 truncate font-mono text-sm font-semibold tabular-nums',
          props.tone
        )}
      >
        {props.value}
      </div>
    </div>
  )
}

function requestContext(
  request: PBRSessionRequest | undefined
): PBRContext | null {
  if (!request?.context?.parts?.length) return null
  return {
    version: 1,
    status: request.context.status as PBRContext['status'],
    tokens: request.context.tokens,
    estimated_tokens: request.context.tokens,
    window: request.context.window,
    counted: request.context.counted,
    held: false,
    turns: 0,
    truncated: false,
    parts: request.context.parts.map((part) => ({
      kind: part.kind,
      tokens: part.tokens,
    })),
  }
}

/**
 * One gateway session (usage-spec §7, ui-spec §6 日志): summary, context fill,
 * and the request timeline. It reads only the gateway's metadata; there is no
 * local agent transcript to show.
 */
export function SessionDetailSheet(props: {
  sessionKey: string | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [order, setOrder] = useState<'asc' | 'desc'>('desc')
  const [selected, setSelected] = useState<PBRRequestLogListItem | null>(null)
  const query = useQuery({
    queryKey: ['pbr-session', props.sessionKey, page, order],
    queryFn: () =>
      getPBRSession(props.sessionKey ?? '', {
        page,
        page_size: 50,
        order,
      }),
    enabled: Boolean(props.sessionKey),
    staleTime: 30_000,
  })
  const detail: PBRSessionDetail | undefined = query.data
  const latestContext = requestContext(
    detail?.request_items.find((request) => request.context?.parts?.length)
  )
  const selectedLog = selected ? mapPBRLogToUsageLog(selected) : null

  return (
    <>
      <Sheet
        open={Boolean(props.sessionKey)}
        onOpenChange={(open) => {
          if (!open) props.onClose()
        }}
      >
        <SheetContent
          side='right'
          className='w-full overflow-y-auto p-0 sm:max-w-3xl'
        >
          <SheetHeader className='border-border/70 bg-background/95 sticky top-0 z-10 border-b px-4 py-3 backdrop-blur'>
            <SheetTitle className='font-mono text-sm'>
              {detail?.session_id || props.sessionKey}
            </SheetTitle>
            <SheetDescription>
              {detail
                ? `${t('First seen')} ${formatTimestampToDate(Math.floor(Date.parse(detail.first_ts) / 1000))} · ${t('Last activity')} ${formatTimestampToDate(Math.floor(Date.parse(detail.last_ts) / 1000))}`
                : t('Gateway conversation')}
            </SheetDescription>
          </SheetHeader>

          <div className='space-y-4 p-4'>
            {query.isLoading && (
              <div className='space-y-2'>
                {[0, 1, 2].map((index) => (
                  <Skeleton key={index} className='h-16 w-full rounded-md' />
                ))}
              </div>
            )}
            {query.isError && (
              <ErrorState
                title={t('Could not load the session')}
                description={
                  query.error instanceof Error ? query.error.message : undefined
                }
                onRetry={() => void query.refetch()}
              />
            )}

            {detail && (
              <>
                <div className='grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4'>
                  <Kpi label={t('Requests')} value={String(detail.requests)} />
                  <Kpi
                    label={t('Errors')}
                    value={String(detail.errors)}
                    tone={detail.errors > 0 ? 'text-rose-600' : undefined}
                  />
                  <Kpi
                    label={t('Tokens')}
                    value={formatTokens(
                      detail.prompt_tokens + detail.completion_tokens
                    )}
                  />
                  <Kpi
                    label={t('Cost')}
                    value={
                      detail.estimated_cost > 0
                        ? formatCostYuan(detail.estimated_cost)
                        : '—'
                    }
                  />
                  <Kpi
                    label={t('Context peak')}
                    value={
                      detail.context_peak_tokens > 0
                        ? formatTokens(detail.context_peak_tokens)
                        : '—'
                    }
                  />
                  <Kpi
                    label={t('Context window')}
                    value={
                      detail.context_window > 0
                        ? formatTokens(detail.context_window)
                        : '—'
                    }
                  />
                  <Kpi
                    label={t('Clients')}
                    value={
                      (detail.client_groups ?? []).length > 0
                        ? detail.client_groups
                            .map((group) =>
                              group === 'script' ? t('Scripts') : group
                            )
                            .join(' · ')
                        : '—'
                    }
                  />
                  <Kpi
                    label={t('Models')}
                    value={
                      (detail.models ?? []).length > 0
                        ? detail.models.join(' · ')
                        : '—'
                    }
                  />
                </div>

                <section className='space-y-2'>
                  <div className='flex items-center justify-between gap-2'>
                    <h3 className='text-sm font-semibold'>{t('Context')}</h3>
                    <a
                      href={`${CONSOLE_ROUTES.logsRequests}?session=${encodeURIComponent(detail.session_key)}`}
                      className='text-primary inline-flex items-center gap-1 text-xs underline-offset-4 hover:underline'
                    >
                      {t('All requests')}
                      <ExternalLink className='size-3' aria-hidden='true' />
                    </a>
                  </div>
                  <PBRContextCard
                    context={latestContext}
                    status={latestContext ? undefined : 'unavailable'}
                  />
                  <ContextSparkline
                    points={detail.context_series ?? []}
                    truncated={detail.context_series_truncated}
                  />
                </section>

                <section className='space-y-2'>
                  <div className='flex items-center justify-between gap-2'>
                    <h3 className='text-sm font-semibold'>
                      {t('Request timeline')}
                    </h3>
                    <Button
                      type='button'
                      variant='ghost'
                      size='sm'
                      className='h-7 px-2 text-xs'
                      onClick={() =>
                        setOrder((current) =>
                          current === 'desc' ? 'asc' : 'desc'
                        )
                      }
                    >
                      {order === 'desc' ? t('Newest first') : t('Oldest first')}
                    </Button>
                  </div>
                  <div className='divide-border/70 divide-y rounded-md border'>
                    {(detail.request_items ?? []).map((request) => (
                      <button
                        key={request.id}
                        type='button'
                        onClick={() => {
                          const { context: _context, ...rest } = request
                          setSelected(rest)
                        }}
                        className='hover:bg-muted/40 flex w-full flex-col gap-1 px-3 py-2 text-left transition-colors'
                      >
                        <div className='flex items-center justify-between gap-2'>
                          <span className='font-mono text-xs tabular-nums'>
                            {formatTimestampToDate(
                              Math.floor(Date.parse(request.ts) / 1000)
                            )}
                          </span>
                          <span
                            className={cn(
                              'text-xs',
                              request.success
                                ? 'text-emerald-600'
                                : 'text-rose-600'
                            )}
                          >
                            {request.success ? t('Successful') : t('Failed')}
                          </span>
                        </div>
                        <div className='text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs'>
                          <span className='font-mono'>
                            {request.request_model}
                          </span>
                          {request.client_name && (
                            <span>{request.client_name}</span>
                          )}
                          <span>
                            {formatTokens(
                              request.prompt_tokens + request.completion_tokens
                            )}
                          </span>
                          {request.estimated_cost > 0 && (
                            <span>
                              {formatCostYuan(request.estimated_cost)}
                            </span>
                          )}
                          {(request.context_tokens ?? 0) > 0 && (
                            <span>
                              {t('Context')}{' '}
                              {formatTokens(request.context_tokens ?? 0)}
                            </span>
                          )}
                        </div>
                      </button>
                    ))}
                    {(detail.request_items ?? []).length === 0 && (
                      <p className='text-muted-foreground px-3 py-6 text-center text-xs'>
                        {t('No requests in this session')}
                      </p>
                    )}
                  </div>
                  {detail.total > 0 && (
                    <div className='flex items-center justify-between gap-2'>
                      <span className='text-muted-foreground text-xs'>
                        {t('Page')} {detail.page} · {detail.page_size} /{' '}
                        {detail.total}
                      </span>
                      <div className='flex items-center gap-1'>
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          className='h-7 px-2'
                          disabled={detail.page <= 1}
                          onClick={() =>
                            setPage((current) => Math.max(1, current - 1))
                          }
                          aria-label={t('Previous page')}
                        >
                          <ChevronLeft
                            className='size-3.5'
                            aria-hidden='true'
                          />
                        </Button>
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          className='h-7 px-2'
                          disabled={
                            detail.page * detail.page_size >= detail.total
                          }
                          onClick={() => setPage((current) => current + 1)}
                          aria-label={t('Next page')}
                        >
                          <ChevronRight
                            className='size-3.5'
                            aria-hidden='true'
                          />
                        </Button>
                      </div>
                    </div>
                  )}
                </section>
              </>
            )}
          </div>
        </SheetContent>
      </Sheet>

      <Dialog
        open={Boolean(selectedLog)}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
        title={t('Request Details')}
        description={
          selectedLog
            ? `${selectedLog.model_name} · ${formatTimestampToDate(selectedLog.created_at)}`
            : undefined
        }
        size='lg'
      >
        {selectedLog && <PBRLogDetailsContent log={selectedLog} />}
      </Dialog>
    </>
  )
}
