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
import { useQuery, useQueryClient, useIsFetching } from '@tanstack/react-query'
import type { Table } from '@tanstack/react-table'
import { LayoutList, Table2 } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getPBRStats } from '@/features/dashboard/pbr-stats-api'
import { useMediaQuery } from '@/hooks'
import { usePageUrlState } from '@/hooks/use-page-url-state'

import type { LogsViewMode } from '../constants'
import { getDefaultTimeRange } from '../lib/utils'
import { CompactDateTimeRangePicker } from './compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from './logs-filter-toolbar'
import { PBRLogsStats } from './pbr-logs-stats'

type SuccessFilter = 'all' | 'true' | 'false'

function maskedFilterLabel(value: SuccessFilter, t: (key: string) => string) {
  if (value === 'true') return t('Masked')
  if (value === 'false') return t('Not masked')
  return t('All masking')
}

interface PBRLogsFilterBarProps<TData> {
  table: Table<TData>
  viewMode?: LogsViewMode
  onViewModeChange?: (mode: LogsViewMode) => void
}

/**
 * PBR 日志筛选栏（ui-spec §6.6）：车道/渠道(名)/令牌/请求模型/成功与否 + 时间范围。
 * PBR 单用户无脱敏需求，无统计头（quota 语义不存在；rpm/tpm 后续接 /api/v1/stats）。
 */
export function PBRLogsFilterBar<TData>(props: PBRLogsFilterBarProps<TData>) {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const { search, navigate } = usePageUrlState()
  const searchParams = search as {
    lane?: string
    channel?: string
    key?: string
    model?: string
    client?: string
    session?: string
    success?: string
    masked?: string
    startTime?: number
    endTime?: number
  }
  const queryClient = useQueryClient()
  const fetchingLogs = useIsFetching({ queryKey: ['logs'] })
  const { start: defaultStart, end: defaultEnd } = useMemo(
    () => getDefaultTimeRange(),
    []
  )

  // 窗口 KPI 与客户端选项跟随**已应用**的时间区间（URL），不跟筛选草稿一起抖动；
  // 未设置时与列表使用同一个默认区间。
  const appliedFrom = Math.floor(
    (searchParams.startTime ?? defaultStart.getTime()) / 1000
  )
  const appliedTo = Math.floor(
    (searchParams.endTime ?? defaultEnd.getTime()) / 1000
  )
  const clientsQuery = useQuery({
    queryKey: ['pbr-client-options', appliedFrom, appliedTo],
    queryFn: () =>
      getPBRStats({
        granularity: 'day',
        from: appliedFrom,
        to: appliedTo,
        groupBy: 'client',
      }),
    staleTime: 60_000,
    refetchOnWindowFocus: false,
  })
  const clientOptions = useMemo(() => {
    const seen = new Map<string, string>()
    for (const bucket of clientsQuery.data?.items ?? []) {
      if (!bucket.group || seen.has(bucket.group)) continue
      seen.set(bucket.group, bucket.client_name || bucket.group)
    }
    return [...seen.entries()].sort((a, b) => a[1].localeCompare(b[1]))
  }, [clientsQuery.data])

  const [draft, setDraft] = useState({
    lane: (searchParams.lane as string) ?? '',
    channel: (searchParams.channel as string) ?? '',
    key: (searchParams.key as string) ?? '',
    model: (searchParams.model as string) ?? '',
    client: (searchParams.client as string) ?? '',
    session: (searchParams.session as string) ?? '',
    success: ((searchParams.success as string) ?? 'all') as SuccessFilter,
    masked: ((searchParams.masked as string) ?? 'all') as SuccessFilter,
    startTime: searchParams.startTime
      ? new Date(searchParams.startTime)
      : defaultStart,
    endTime: searchParams.endTime ? new Date(searchParams.endTime) : defaultEnd,
  })

  const applyWith = useCallback(
    (overrides: Partial<typeof draft> = {}) => {
      const next = { ...draft, ...overrides }
      navigate({
        search: {
          page: 1,
          lane: next.lane || undefined,
          channel: next.channel || undefined,
          key: next.key || undefined,
          model: next.model || undefined,
          client: next.client || undefined,
          session: next.session || undefined,
          success: next.success === 'all' ? undefined : next.success,
          masked: next.masked === 'all' ? undefined : next.masked,
          startTime: next.startTime.getTime(),
          endTime: next.endTime.getTime(),
        },
      })
      queryClient.invalidateQueries({ queryKey: ['logs'] })
    },
    [draft, navigate, queryClient]
  )

  const handleChange = useCallback(
    (field: keyof typeof draft, value: string | Date) => {
      setDraft((current) => ({ ...current, [field]: value }))
    },
    []
  )

  const handleReset = useCallback(() => {
    const { start, end } = getDefaultTimeRange()
    setDraft({
      lane: '',
      channel: '',
      key: '',
      model: '',
      client: '',
      session: '',
      success: 'all',
      masked: 'all',
      startTime: start,
      endTime: end,
    })
    navigate({
      search: { page: 1, startTime: start.getTime(), endTime: end.getTime() },
    })
    queryClient.invalidateQueries({ queryKey: ['logs'] })
  }, [navigate, queryClient])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') applyWith()
    },
    [applyWith]
  )

  const hasActiveFilters =
    !!draft.lane ||
    !!draft.channel ||
    !!draft.key ||
    !!draft.model ||
    !!draft.client ||
    !!draft.session ||
    draft.success !== 'all' ||
    draft.masked !== 'all'

  const dateRangeFilter = (
    <LogsFilterField>
      <CompactDateTimeRangePicker
        start={draft.startTime}
        end={draft.endTime}
        onChange={({ start, end }) => {
          if (!start || !end) return
          const overrides = { startTime: start, endTime: end }
          setDraft((current) => ({ ...current, ...overrides }))
          if (isMobile) applyWith(overrides)
        }}
      />
    </LogsFilterField>
  )
  const laneFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Route')}
        value={draft.lane}
        onChange={(e) => handleChange('lane', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  const channelFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Channel')}
        value={draft.channel}
        onChange={(e) => handleChange('channel', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  const keyFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Key')}
        value={draft.key}
        onChange={(e) => handleChange('key', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  const modelFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Request model')}
        value={draft.model}
        onChange={(e) => handleChange('model', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  const clientFilter = (
    <LogsFilterField>
      <Select
        value={draft.client || 'all'}
        onValueChange={(value) => {
          const next = (value ?? 'all') === 'all' ? '' : String(value)
          setDraft((current) => ({ ...current, client: next }))
        }}
      >
        <SelectTrigger className='h-8 w-36' aria-label={t('Client filter')}>
          <SelectValue>
            {draft.client
              ? (clientOptions.find(([value]) => value === draft.client)?.[1] ??
                draft.client)
              : t('All clients')}
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value='all'>{t('All clients')}</SelectItem>
          {clientOptions.map(([value, label]) => (
            <SelectItem key={value} value={value}>
              {label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </LogsFilterField>
  )
  const sessionFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Session')}
        value={draft.session}
        onChange={(e) => handleChange('session', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  // 成功与否筛选的标签（单层判定构建，web/AGENTS §3.2 禁嵌套三元）。
  const successLabelMap: Record<SuccessFilter, string> = {
    all: t('All statuses'),
    true: t('Successful'),
    false: t('Failed'),
  }
  const maskedLabel = maskedFilterLabel(draft.masked, t)
  const maskedFilter = (
    <LogsFilterField>
      <Select
        value={draft.masked}
        onValueChange={(value) => {
          const v = (value ?? 'all') as SuccessFilter
          setDraft((current) => ({ ...current, masked: v }))
        }}
      >
        <SelectTrigger className='h-8 w-32' aria-label={t('Masking filter')}>
          <SelectValue>{maskedLabel}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value='all'>{t('All masking')}</SelectItem>
          <SelectItem value='true'>{t('Masked')}</SelectItem>
          <SelectItem value='false'>{t('Not masked')}</SelectItem>
        </SelectContent>
      </Select>
    </LogsFilterField>
  )
  const successFilter = (
    <LogsFilterField>
      <Select
        value={draft.success}
        onValueChange={(value) => {
          const v = (value ?? 'all') as SuccessFilter
          setDraft((current) => ({ ...current, success: v }))
        }}
      >
        <SelectTrigger className='h-8 w-36' aria-label={t('Status filter')}>
          {/* 显式给 SelectValue 子节点：Base UI 默认回显原始值（all/true/false），
              会绕过 i18n。 */}
          <SelectValue>{successLabelMap[draft.success]}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value='all'>{t('All statuses')}</SelectItem>
          <SelectItem value='true'>{t('Successful')}</SelectItem>
          <SelectItem value='false'>{t('Failed')}</SelectItem>
        </SelectContent>
      </Select>
    </LogsFilterField>
  )

  const viewToggle =
    props.viewMode && props.onViewModeChange ? (
      <div
        role='group'
        aria-label={t('Log view')}
        className='border-border/70 bg-muted/20 inline-flex items-center gap-0.5 rounded-md border p-0.5'
      >
        <Button
          type='button'
          variant={props.viewMode === 'table' ? 'secondary' : 'ghost'}
          size='sm'
          className='h-7 gap-1 px-2'
          aria-pressed={props.viewMode === 'table'}
          onClick={() => props.onViewModeChange?.('table')}
        >
          <Table2 className='size-3.5' aria-hidden='true' />
          {t('Table')}
        </Button>
        <Button
          type='button'
          variant={props.viewMode === 'cards' ? 'secondary' : 'ghost'}
          size='sm'
          className='h-7 gap-1 px-2'
          aria-pressed={props.viewMode === 'cards'}
          onClick={() => props.onViewModeChange?.('cards')}
        >
          <LayoutList className='size-3.5' aria-hidden='true' />
          {t('Cards')}
        </Button>
      </div>
    ) : undefined

  const stats = <PBRLogsStats from={appliedFrom} to={appliedTo} />

  return (
    <LogsFilterToolbar
      table={props.table}
      compactMobile
      stats={stats}
      viewToggle={viewToggle}
      primaryFilters={
        <>
          {dateRangeFilter}
          {laneFilter}
          {successFilter}
        </>
      }
      advancedFilters={
        <>
          {modelFilter}
          {channelFilter}
          {keyFilter}
          {clientFilter}
          {sessionFilter}
          {maskedFilter}
        </>
      }
      mobilePinnedFilters={dateRangeFilter}
      mobileFilters={
        <>
          {laneFilter}
          {successFilter}
          {modelFilter}
          {channelFilter}
          {keyFilter}
          {clientFilter}
          {sessionFilter}
          {maskedFilter}
        </>
      }
      mobileFilterCount={
        [
          draft.lane,
          draft.success !== 'all',
          draft.model,
          draft.channel,
          draft.key,
          draft.client,
          draft.session,
          draft.masked !== 'all',
        ].filter(Boolean).length
      }
      hasAdvancedActiveFilters={
        !!draft.model ||
        !!draft.channel ||
        !!draft.key ||
        !!draft.client ||
        !!draft.session ||
        draft.masked !== 'all'
      }
      advancedFilterCount={
        [
          draft.model,
          draft.channel,
          draft.key,
          draft.client,
          draft.session,
          draft.masked !== 'all',
        ].filter(Boolean).length
      }
      hasActiveFilters={hasActiveFilters}
      onSearch={() => applyWith()}
      searchLoading={fetchingLogs > 0}
      onReset={handleReset}
    />
  )
}
