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
import type { Table } from '@tanstack/react-table'
import { ArrowRight, ChevronDown, GitBranch } from 'lucide-react'
import { type ReactNode, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { UsageLog } from '../data/schema'
import { formatModelName, parseLogOther } from '../lib/format'
import { getLogTypeConfig } from '../lib/utils'
import { ModelBadge } from './model-badge'
import { PBRLogDetailsContent } from './pbr-log-details'
import { useUsageLogsContext } from './usage-logs-provider'

type PBRLogCardListProps = {
  table: Table<Record<string, unknown>>
  isLoading: boolean
  emptyTitle: string
  emptyDescription: string
  emptyAction?: ReactNode
}

/**
 * 请求日志桌面「卡片」视图（ui-spec §6 日志）。
 *
 * 与表格视图共用同一个 TanStack table（筛选/分页/实时 tail 完全一致），只替换
 * 行的呈现：一行一张卡，先读 `路由 → 渠道 / 模型`，再读时间与耗时/Token/费用。
 * 默认折叠详情，展开复用 `PBRLogDetailsContent`，不弹二级对话框。
 */
export function PBRLogCardList(props: PBRLogCardListProps) {
  const { t } = useTranslation()
  const rows = props.table.getRowModel().rows

  if (props.isLoading) {
    return (
      <div className='space-y-2' aria-busy='true'>
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton key={index} className='rounded-panel h-[92px]' />
        ))}
      </div>
    )
  }

  if (rows.length === 0) {
    return (
      <EmptyState
        title={props.emptyTitle}
        description={props.emptyDescription}
        action={props.emptyAction}
        className='min-h-[320px]'
      />
    )
  }

  return (
    <ul className='space-y-2' aria-label={t('Request logs')}>
      {rows.map((row) => (
        <li key={row.id}>
          <PBRLogCard log={row.original as unknown as UsageLog} />
        </li>
      ))}
    </ul>
  )
}

function PBRLogCard(props: { log: UsageLog }) {
  const { t } = useTranslation()
  const { sensitiveVisible } = useUsageLogsContext()
  const [expanded, setExpanded] = useState(false)
  const log = props.log
  const other = parseLogOther(log.other)
  const pbr = other?.pbr
  const model = formatModelName(log)
  const config = getLogTypeConfig(log.type)
  const lane = pbr?.lane
  const channelName = sensitiveVisible
    ? log.channel_name || `#${log.channel}`
    : `#${log.channel}`
  const tokenName = sensitiveVisible ? log.token_name : '••••'
  const cost = pbr?.estimated_cost ?? 0
  const ttft = other?.frt ?? 0
  const totalMs = pbr?.total_ms ?? log.use_time * 1000
  const totalAttempts = pbr?.total_attempts ?? 0

  return (
    <article
      className={cn(
        'border-border/70 bg-card rounded-panel border px-3 py-2.5 shadow-xs transition-colors sm:px-4 sm:py-3',
        'hover:border-border'
      )}
    >
      <div className='flex min-w-0 items-start gap-3'>
        <StatusBadge
          label={t(config.label)}
          variant={config.color as StatusVariant}
          size='sm'
          copyable={false}
          showDot
          className='mt-0.5 shrink-0'
        />

        <div className='min-w-0 flex-1'>
          {/* 路由 → 渠道 / 模型：卡片视图的第一阅读序。 */}
          <div className='flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 text-xs'>
            <span
              className='text-muted-foreground max-w-[18rem] truncate font-mono'
              title={lane || undefined}
            >
              {lane || t('No route')}
            </span>
            <ArrowRight
              className='text-muted-foreground/60 size-3 shrink-0'
              aria-hidden='true'
            />
            <span
              className='text-muted-foreground max-w-[14rem] truncate font-mono'
              title={channelName}
            >
              {channelName}
            </span>
            <ArrowRight
              className='text-muted-foreground/60 size-3 shrink-0'
              aria-hidden='true'
            />
            <ModelBadge
              modelName={model.name}
              actualModel={model.actualModel}
            />
          </div>

          <div className='text-muted-foreground mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums'>
            <span>{formatTimestampToDate(log.created_at)}</span>
            {tokenName && (
              <span className='max-w-[10rem] truncate' title={tokenName}>
                {tokenName}
              </span>
            )}
            {ttft > 0 && (
              <span>
                TTFT <span className='text-foreground'>{ttft} ms</span>
              </span>
            )}
            {totalMs > 0 && (
              <span>
                {t('Total')}{' '}
                <span className='text-foreground'>
                  {(totalMs / 1000).toFixed(2)} s
                </span>
              </span>
            )}
            {log.prompt_tokens > 0 && (
              <span>
                {t('Input')}{' '}
                <span className='text-foreground'>
                  {log.prompt_tokens.toLocaleString()}
                </span>
              </span>
            )}
            {log.completion_tokens > 0 && (
              <span>
                {t('Output')}{' '}
                <span className='text-foreground'>
                  {log.completion_tokens.toLocaleString()}
                </span>
              </span>
            )}
            {cost > 0 && (
              <span>
                {t('Cost')}{' '}
                <span className='text-foreground font-medium'>
                  ¥{cost.toFixed(4)}
                </span>
              </span>
            )}
            {totalAttempts > 1 && (
              <span className='text-warning inline-flex items-center gap-1'>
                <GitBranch className='size-3' aria-hidden='true' />
                {t('{{count}} attempts', { count: totalAttempts })}
              </span>
            )}
          </div>

          {pbr?.error_summary && (
            <p className='text-destructive mt-1 line-clamp-1 text-xs'>
              {pbr.error_kind ? `${pbr.error_kind}: ` : ''}
              {pbr.error_summary}
            </p>
          )}
        </div>

        {pbr && (
          <Button
            variant='ghost'
            size='icon'
            className='text-muted-foreground size-7 shrink-0'
            onClick={() => setExpanded((value) => !value)}
            aria-expanded={expanded}
            aria-label={expanded ? t('Collapse') : t('Details')}
          >
            <ChevronDown
              className={cn(
                'size-4 transition-transform',
                expanded && 'rotate-180'
              )}
              aria-hidden='true'
            />
          </Button>
        )}
      </div>

      {expanded && pbr && (
        <div className='border-border/70 mt-3 border-t pt-3'>
          <PBRLogDetailsContent log={log} />
        </div>
      )}
    </article>
  )
}
