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

import { formatTokens } from '@/lib/format'
import { cn } from '@/lib/utils'

import type {
  PBRContext,
  PBRContextPart,
  PBRContextStatus,
} from '../pbr/pbr-logs-api'

const PART_ORDER = [
  'system',
  'tools',
  'skills',
  'memory',
  'files',
  'results',
  'chat',
] as const

const PART_TONE: Record<string, string> = {
  system: 'bg-sky-500',
  tools: 'bg-amber-500',
  skills: 'bg-violet-500',
  memory: 'bg-emerald-500',
  files: 'bg-rose-500',
  results: 'bg-orange-500',
  chat: 'bg-slate-500',
}

const PART_LABEL: Record<string, string> = {
  system: 'System prompt',
  tools: 'Tools',
  skills: 'Skills',
  memory: 'Memory',
  files: 'Files',
  results: 'Tool results',
  chat: 'Conversation',
}

function statusKey(status: PBRContextStatus | string | undefined): string {
  switch (status) {
    case 'unsupported':
      return 'Context is not available for this request type'
    case 'too_large':
      return 'Request body is too large to inspect'
    case 'unavailable':
      return 'Context could not be read'
    default:
      return 'No context was recorded for this request'
  }
}

function fillTone(ratio: number): string {
  if (ratio >= 0.9) return 'bg-rose-500'
  if (ratio >= 0.75) return 'bg-amber-500'
  return 'bg-emerald-500'
}

function partsWithOrder(parts: PBRContextPart[] | undefined): PBRContextPart[] {
  const byKind = new Map((parts ?? []).map((part) => [part.kind, part]))
  return PART_ORDER.map((kind) => byKind.get(kind)).filter(
    (part): part is PBRContextPart => Boolean(part && part.tokens > 0)
  )
}

/**
 * A request's prompt composition (usage-spec §5, ui-spec §6 日志详情).
 *
 * Only names, tags and token counts are shown; a request without context
 * shows a reason, never a fabricated 0% fill.
 */
export function PBRContextCard(props: {
  context?: PBRContext | null
  status?: PBRContextStatus | string
  tokens?: number
  window?: number
  counted?: boolean
  compact?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  const context = props.context ?? null
  const status = context?.status ?? props.status ?? ''
  const tokens = context?.tokens ?? props.tokens ?? 0
  const window = context?.window ?? props.window ?? 0
  const counted = context?.counted ?? props.counted ?? false
  const parts = partsWithOrder(context?.parts)
  const ratio = window > 0 ? Math.min(1, tokens / window) : null

  if (!context) {
    return (
      <div
        className={cn(
          'text-muted-foreground rounded-md border border-dashed p-3 text-xs',
          props.className
        )}
      >
        {t(statusKey(status))}
      </div>
    )
  }

  if (tokens <= 0 && parts.length === 0) {
    return (
      <div
        className={cn(
          'text-muted-foreground rounded-md border border-dashed p-3 text-xs',
          props.className
        )}
      >
        {t('No context was recorded for this request')}
      </div>
    )
  }

  return (
    <div
      className={cn(
        'min-w-0 space-y-2 rounded-md border bg-muted/20 p-3',
        props.className
      )}
    >
      <div className='flex flex-wrap items-baseline justify-between gap-2'>
        <div className='flex items-baseline gap-2'>
          <span className='text-xs font-semibold'>{t('Context window')}</span>
          <span className='font-mono text-sm tabular-nums'>
            {formatTokens(tokens)}
          </span>
          {window > 0 && (
            <span className='text-muted-foreground text-xs tabular-nums'>
              {t('of')} {formatTokens(window)} (
              {Math.round((tokens / window) * 100)}%)
            </span>
          )}
        </div>
        <span className='text-muted-foreground text-xs'>
          {counted ? t('Counted by upstream') : t('Estimated')}
          {context.held ? ` · ${t('Vendor-held conversation')}` : ''}
        </span>
      </div>

      {ratio != null && (
        <div
          className='bg-muted h-2 overflow-hidden rounded-full'
          role='img'
          aria-label={`${t('Context window')} ${Math.round(ratio * 100)}%`}
        >
          <div
            className={cn('h-full rounded-full', fillTone(ratio))}
            style={{ width: `${Math.max(1, Math.round(ratio * 100))}%` }}
          />
        </div>
      )}

      {parts.length > 0 && (
        <div className='flex h-2 overflow-hidden rounded-full'>
          {parts.map((part) => (
            <div
              key={part.kind}
              className={cn(PART_TONE[part.kind] ?? 'bg-slate-400')}
              style={{ width: `${(part.tokens / tokens) * 100}%` }}
              title={`${t(PART_LABEL[part.kind] ?? part.kind)}: ${formatTokens(part.tokens)}`}
            />
          ))}
        </div>
      )}

      {!props.compact && (
        <div className='grid gap-2 sm:grid-cols-2'>
          {parts.map((part) => (
            <div
              key={part.kind}
              className='bg-background/60 min-w-0 rounded-md border p-2'
            >
              <div className='flex items-center justify-between gap-2 text-xs'>
                <span className='flex min-w-0 items-center gap-1.5'>
                  <span
                    className={cn(
                      'size-2 shrink-0 rounded-full',
                      PART_TONE[part.kind] ?? 'bg-slate-400'
                    )}
                    aria-hidden='true'
                  />
                  <span className='truncate font-medium'>
                    {t(PART_LABEL[part.kind] ?? part.kind)}
                  </span>
                </span>
                <span className='text-muted-foreground shrink-0 font-mono tabular-nums'>
                  {formatTokens(part.tokens)}
                </span>
              </div>
              {part.items && part.items.length > 0 && (
                <ul className='mt-1.5 max-h-40 space-y-1 overflow-auto'>
                  {part.items.map((item) => (
                    <li
                      key={`${part.kind}-${item.tag ?? ''}-${item.name}`}
                      className='flex items-baseline justify-between gap-2 text-xs'
                      title={item.name}
                    >
                      <span className='min-w-0 truncate font-mono'>
                        {item.name}
                        {item.n && item.n > 0 ? ` ×${item.n}` : ''}
                      </span>
                      <span className='text-muted-foreground shrink-0 font-mono tabular-nums'>
                        {formatTokens(item.tokens)}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          ))}
        </div>
      )}

      {context.truncated && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Context details were truncated to stay within the metadata limit'
          )}
        </p>
      )}
    </div>
  )
}
