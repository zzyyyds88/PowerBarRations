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
import { useQueryClient, useIsFetching } from '@tanstack/react-query'
import { useNavigate, getRouteApi } from '@tanstack/react-router'
import type { Table } from '@tanstack/react-table'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useMediaQuery } from '@/hooks'

import { getDefaultTimeRange } from '../lib/utils'
import { CompactDateTimeRangePicker } from './compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from './logs-filter-toolbar'

const route = getRouteApi('/_authenticated/usage-logs/$section')

type SuccessFilter = 'all' | 'true' | 'false'

interface PBRLogsFilterBarProps<TData> {
  table: Table<TData>
}

/**
 * PBR 日志筛选栏（ui-spec §6.6）：车道/渠道(名)/令牌/请求模型/成功与否 + 时间范围。
 * PBR 单用户无脱敏需求，无统计头（quota 语义不存在；rpm/tpm 后续接 /api/v1/stats）。
 */
export function PBRLogsFilterBar<TData>(props: PBRLogsFilterBarProps<TData>) {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const searchParams = route.useSearch()
  const fetchingLogs = useIsFetching({ queryKey: ['logs'] })

  const { start: defaultStart, end: defaultEnd } = useMemo(
    () => getDefaultTimeRange(),
    []
  )
  const [draft, setDraft] = useState({
    lane: (searchParams.lane as string) ?? '',
    channel: (searchParams.channel as string) ?? '',
    key: (searchParams.key as string) ?? '',
    model: (searchParams.model as string) ?? '',
    success: ((searchParams.success as string) ?? 'all') as SuccessFilter,
    startTime: searchParams.startTime
      ? new Date(searchParams.startTime)
      : defaultStart,
    endTime: searchParams.endTime ? new Date(searchParams.endTime) : defaultEnd,
  })

  const applyWith = useCallback(
    (overrides: Partial<typeof draft> = {}) => {
      const next = { ...draft, ...overrides }
      navigate({
        to: '/usage-logs/$section',
        params: { section: 'pbr' },
        search: {
          page: 1,
          lane: next.lane || undefined,
          channel: next.channel || undefined,
          key: next.key || undefined,
          model: next.model || undefined,
          success: next.success === 'all' ? undefined : next.success,
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
      success: 'all',
      startTime: start,
      endTime: end,
    })
    navigate({
      to: '/usage-logs/$section',
      params: { section: 'pbr' },
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
    draft.success !== 'all'

  const dateRangeFilter = (
    <LogsFilterField wide>
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
        placeholder={t('Lane')}
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
  const successFilter = (
    <LogsFilterField>
      <Select
        value={draft.success}
        onValueChange={(value) => {
          const v = (value ?? 'all') as SuccessFilter
          setDraft((current) => ({ ...current, success: v }))
        }}
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
    </LogsFilterField>
  )

  return (
    <LogsFilterToolbar
      table={props.table}
      compactMobile
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
        </>
      }
      mobileFilterCount={
        [
          draft.lane,
          draft.success !== 'all',
          draft.model,
          draft.channel,
          draft.key,
        ].filter(Boolean).length
      }
      hasAdvancedActiveFilters={!!draft.model || !!draft.channel || !!draft.key}
      advancedFilterCount={
        [draft.model, draft.channel, draft.key].filter(Boolean).length
      }
      hasActiveFilters={hasActiveFilters}
      onSearch={() => applyWith()}
      searchLoading={fetchingLogs > 0}
      onReset={handleReset}
    />
  )
}
