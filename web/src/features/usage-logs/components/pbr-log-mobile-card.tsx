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
/**
 * PBR 请求日志的移动端卡片（ui-spec §6.6 移动端卡片视图）。
 *
 * `UsageLogsMobileList` 在 `pbr` 分支用本组件替代空壳：把桌面表格的 10 列
 * （时间/渠道/令牌/模型/车道/流/Tokens/费用/用时/详情）重排为竖排卡片，
 * 避免窄屏横向滚动；详情就地展开 `PBRLogDetailsContent`（不开弹窗）。
 */
import { flexRender, type Cell } from '@tanstack/react-table'
import { ChevronDown, KeyRound } from 'lucide-react'
import { type ReactNode, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import dayjs from '@/lib/dayjs'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { UsageLog } from '../data/schema'
import { formatModelName, parseLogOther } from '../lib/format'
import {
  getLogTypeConfig,
  isDisplayableLogType,
  isTimingLogType,
} from '../lib/utils'
import { ModelBadge } from './model-badge'
import { PBRLogDetailsContent } from './pbr-log-details'
import { StreamTpsCell, TimingMetricsCell } from './timing-metrics-cell'
import { useUsageLogsContext } from './usage-logs-provider'

type FieldName = 'model' | 'channel' | 'token' | 'time'
type LogField = {
  label: string
  value: string
  visible: boolean
  sensitive?: boolean
}

export function PBRLogMobileCard<TData>(props: {
  log: UsageLog
  cells: Map<string, Cell<TData, unknown>>
}) {
  const { t } = useTranslation()
  const context = useUsageLogsContext()
  const [selectedField, setSelectedField] = useState<FieldName | null>(null)
  const [expanded, setExpanded] = useState(false)
  const log = props.log
  const other = parseLogOther(log.other)
  const pbr = other?.pbr
  const displayable = isDisplayableLogType(log.type)
  const timing = isTimingLogType(log.type)
  const model = formatModelName(log)
  const config = getLogTypeConfig(log.type)

  const fields: Record<FieldName, LogField> = {
    model: {
      label: t('Model'),
      value: model.name,
      visible: displayable && props.cells.has('model_name') && !!model.name,
    },
    time: {
      label: t('Time'),
      value: formatTimestampToDate(log.created_at),
      visible: props.cells.has('created_at'),
    },
    channel: {
      label: t('Channel'),
      value: [log.channel_name, `#${log.channel}`].filter(Boolean).join(' '),
      visible: displayable && props.cells.has('channel'),
      sensitive: true,
    },
    token: {
      label: t('Token'),
      value: log.token_name,
      visible: displayable && props.cells.has('token_name') && !!log.token_name,
      sensitive: true,
    },
  }
  const selected = selectedField ? fields[selectedField] : undefined
  const activeField =
    selected?.visible && (!selected.sensitive || context.sensitiveVisible)
      ? selected
      : undefined

  const channelCell = props.cells.get('channel')
  const lane = pbr?.lane
  const showLane = displayable && !!lane && props.cells.has('lane')
  const cost = pbr?.estimated_cost
  const showCost =
    displayable && props.cells.has('quota') && cost != null && cost !== 0

  const cacheRead = other?.cache_tokens || 0
  const cacheWrite =
    (other?.cache_creation_tokens_5m || 0) +
      (other?.cache_creation_tokens_1h || 0) ||
    other?.cache_creation_tokens ||
    0
  const showTokens =
    displayable &&
    props.cells.has('prompt_tokens') &&
    (log.prompt_tokens > 0 ||
      log.completion_tokens > 0 ||
      cacheRead > 0 ||
      cacheWrite > 0)

  return (
    <div className='min-w-0 space-y-2.5 text-sm leading-5'>
      {/* 模型 */}
      {fields.model.visible && (
        <div className='flex min-w-0 flex-wrap items-start gap-x-3 gap-y-2'>
          <div className='min-w-0 flex-[1_1_10rem]'>
            <ModelBadge
              modelName={model.name}
              actualModel={model.actualModel}
              wrapText
              onInspect={() => setSelectedField('model')}
            />
          </div>
        </div>
      )}

      {/* 时间 + 状态 / 流 + 用时 */}
      <div
        className='grid min-w-0 grid-cols-2 items-stretch gap-x-3'
        data-slot='log-time-and-timing'
      >
        {fields.time.visible && (
          <div className='flex min-w-0 flex-col items-start justify-between gap-1'>
            <StatusBadge
              label={t(config.label)}
              variant={config.color as StatusVariant}
              copyable={false}
              showDot
              className='h-5 px-0 text-xs'
            />
            <Button
              variant='ghost'
              aria-label={`${t('Time')}: ${fields.time.value}`}
              aria-haspopup='dialog'
              onClick={() => setSelectedField('time')}
              className='text-muted-foreground h-auto min-h-6 px-0 py-0 text-xs font-normal whitespace-normal tabular-nums'
            >
              {dayjs.unix(log.created_at).format('MM-DD HH:mm:ss')}
            </Button>
          </div>
        )}
        {timing &&
          (props.cells.has('use_time') || props.cells.has('is_stream')) && (
            <div className='col-start-2 flex min-w-0 flex-col items-end gap-1 [overflow-wrap:anywhere]'>
              {props.cells.has('is_stream') && (
                <StreamTpsCell
                  compact
                  className='min-h-5 max-w-full min-w-0 justify-end'
                  isStream={log.is_stream}
                  tokensPerSecond={
                    log.use_time > 0 && log.completion_tokens > 0
                      ? log.completion_tokens / log.use_time
                      : null
                  }
                  streamStatus={other?.stream_status}
                />
              )}
              {props.cells.has('use_time') && (
                <TimingMetricsCell
                  useTimeSec={log.use_time}
                  completionTokens={log.completion_tokens}
                  frtMs={other?.frt}
                  isStream={log.is_stream}
                  indicator='dot'
                  compact
                  className='min-h-6 max-w-full min-w-0 items-center justify-end [&>div]:justify-end'
                />
              )}
            </div>
          )}
      </div>

      {/* 车道 / 渠道 / 令牌 / 费用：两列键值网格 */}
      <div className='grid min-w-0 grid-cols-2 gap-x-4 gap-y-0.5'>
        {showLane && (
          <KeyValue label={t('Lane')}>
            <span className='font-mono text-xs [overflow-wrap:anywhere]'>
              {lane}
            </span>
          </KeyValue>
        )}
        {fields.channel.visible && (
          <KeyValue label={fields.channel.label}>
            {context.sensitiveVisible ? (
              <Button
                variant='ghost'
                aria-label={`${fields.channel.label}: ${fields.channel.value}`}
                aria-haspopup='dialog'
                onClick={() => setSelectedField('channel')}
                className='text-foreground h-auto min-h-8 min-w-0 flex-1 shrink justify-start px-0 py-1 text-left text-sm font-normal'
              >
                <span className='truncate'>{fields.channel.value}</span>
              </Button>
            ) : (
              <span className='min-w-0 py-1.5'>••••</span>
            )}
          </KeyValue>
        )}
        {fields.token.visible && (
          <KeyValue label={fields.token.label}>
            {context.sensitiveVisible ? (
              <Button
                variant='ghost'
                aria-label={`${fields.token.label}: ${fields.token.value}`}
                aria-haspopup='dialog'
                onClick={() => setSelectedField('token')}
                className='text-foreground h-auto min-h-8 min-w-0 flex-1 shrink justify-start px-0 py-1 text-left text-sm font-normal'
              >
                <StatusBadge
                  label={fields.token.value}
                  copyable={false}
                  icon={KeyRound}
                  className='border-border/60 bg-muted/30 text-foreground max-w-full rounded-md border px-1.5 py-0.5 text-sm'
                />
              </Button>
            ) : (
              <span className='min-w-0 py-1.5'>••••</span>
            )}
          </KeyValue>
        )}
        {showCost && (
          <KeyValue label={t('Cost')}>
            <span className='font-mono text-xs font-medium tabular-nums'>
              {context.sensitiveVisible ? (cost ?? 0).toFixed(4) : '••••'}
            </span>
          </KeyValue>
        )}
      </div>

      {/* Tokens */}
      {showTokens && (
        <div className='text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 text-xs [overflow-wrap:anywhere]'>
          <span>
            {t('Input')}{' '}
            <span className='text-foreground tabular-nums'>
              {log.prompt_tokens.toLocaleString()}
            </span>
          </span>
          <span>
            {t('Output')}{' '}
            <span className='text-foreground tabular-nums'>
              {log.completion_tokens.toLocaleString()}
            </span>
          </span>
          {cacheRead > 0 && (
            <span>
              {t('Cache')} ↓ {cacheRead.toLocaleString()}
            </span>
          )}
          {cacheWrite > 0 && (
            <span>
              {t('Cache')} ↑ {cacheWrite.toLocaleString()}
            </span>
          )}
        </div>
      )}

      {/* 详情就地展开 */}
      {props.cells.has('content') && pbr && (
        <div className='border-t pt-1'>
          <button
            type='button'
            onClick={() => setExpanded((v) => !v)}
            aria-expanded={expanded}
            className='text-muted-foreground hover:text-foreground flex w-full items-center justify-center gap-1 rounded py-1 text-xs transition-colors'
          >
            <ChevronDown
              className={cn(
                'size-3.5 transition-transform',
                expanded && 'rotate-180'
              )}
              aria-hidden='true'
            />
            {expanded ? t('Collapse') : t('Details')}
          </button>
          {expanded && (
            <div className='mt-1'>
              <PBRLogDetailsContent log={log} />
            </div>
          )}
        </div>
      )}

      <Dialog
        size='md'
        open={!!activeField}
        onOpenChange={(open) => {
          if (!open) setSelectedField(null)
        }}
        title={activeField?.label ?? t('Details')}
        contentClassName='max-sm:top-auto max-sm:bottom-0 max-sm:max-h-[85dvh] max-sm:max-w-full max-sm:translate-y-0 max-sm:rounded-b-none max-sm:rounded-t-2xl max-sm:pb-[max(1rem,env(safe-area-inset-bottom))] [&_[data-slot=dialog-close]]:size-11'
        footer={
          activeField && (
            <CopyButton
              value={activeField.value}
              variant='default'
              size='default'
              className='min-h-11 w-full'
            >
              {t('Copy')}
            </CopyButton>
          )
        }
      >
        {activeField && (
          <div className='space-y-4'>
            <p className='bg-muted rounded-lg p-4 text-base [overflow-wrap:anywhere] whitespace-pre-wrap'>
              {activeField.value}
            </p>
            {selectedField === 'model' && model.actualModel && (
              <div className='space-y-2'>
                <p className='text-muted-foreground'>{t('Actual Model')}</p>
                <p className='text-base [overflow-wrap:anywhere]'>
                  {model.actualModel}
                </p>
                <CopyButton value={model.actualModel} />
              </div>
            )}
            {selectedField === 'channel' && channelCell && (
              <div>
                {flexRender(
                  channelCell.column.columnDef.cell,
                  channelCell.getContext()
                )}
              </div>
            )}
          </div>
        )}
      </Dialog>
    </div>
  )
}

function KeyValue({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className='flex min-w-0 items-center gap-2'>
      <span className='text-muted-foreground max-w-[40%] shrink-0 text-xs [overflow-wrap:anywhere]'>
        {label}
      </span>
      <span className='min-w-0 flex-1'>{children}</span>
    </div>
  )
}
