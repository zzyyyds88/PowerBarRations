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
/*
实时路由卡（magpie 风格）：车道 → 网关 → 成员，右侧是运行态结论。

数据源（全部只读，无后端改动）：
- 车道列表：GET /api/v1/lane-summaries；
- 运行态：SSE /api/v1/route-events + 30s 健康轮询（useLaneRuntime）；
- 车道策略六键：GET /api/v1/lanes/{name}（失败回落内置默认）；
- 车道窗口统计：GET /api/stats?group_by=lane（小时桶，from/to 在 queryFn 内对齐）；
- 最近一次请求：GET /api/v1/logs?lane=...&limit=1（含 attempts 链）。
*/
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { RefreshCw, Waypoints } from 'lucide-react'
import { useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { Logo } from '@/assets/logo'
import { Button, buttonVariants } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { getPBRStats } from '@/features/dashboard/pbr-stats-api'
import {
  getPBRLane,
  listPBRLaneSummaries,
  type PBRLaneSummary,
} from '@/features/routes/api'
import { useLaneRuntime } from '@/features/routes/hooks/use-lane-runtime'
import { listPBRRequestLogs } from '@/features/usage-logs/pbr/pbr-logs-api'
import { getChartColor } from '@/lib/colors'
import { cn } from '@/lib/utils'

import {
  buildMemberRows,
  firstAvailableMember,
  laneStatsMap,
  pickDefaultLane,
  resolveLanePolicy,
  sortLanesForRail,
  type LaneWindowStats,
} from '../lib/live-routing'
import { LaneRail } from './live-routing-lane-rail'
import { MemberRows } from './live-routing-members'
import { LiveRoutingFooter } from './live-routing-recent'
import { RoutingConnectors } from './routing-connectors'

const EMPTY_STATS: LaneWindowStats = {
  requests: 0,
  failures: 0,
  successRate: null,
}

const LIVE_QUERY_PREFIX = ['pbr-channel-status-live'] as const

/** 实时路由卡的统计窗口；由所在页面（概览 / 渠道状态）注入。 */
export interface LiveRoutingStatsRange {
  from: number
  to: number
  /** 稳定的查询键：窗口变化才换键，避免每秒重取。 */
  key: string
  granularity?: 'hour' | 'day'
}

function LiveRoutingSkeleton() {
  return (
    <div className='grid gap-4 lg:grid-cols-[210px_120px_minmax(0,1fr)] lg:gap-8'>
      <div className='flex flex-col gap-2'>
        {['one', 'two', 'three', 'four'].map((key) => (
          <Skeleton key={key} className='h-[74px] rounded-lg' />
        ))}
      </div>
      <div className='hidden items-center justify-center lg:flex'>
        <Skeleton className='size-20 rounded-2xl' />
      </div>
      <div className='flex flex-col gap-2'>
        {['a', 'b', 'c', 'd', 'e'].map((key) => (
          <Skeleton key={key} className='h-[62px] rounded-lg' />
        ))}
      </div>
    </div>
  )
}

function LiveBadge(props: { connected: boolean }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full border px-1.5 py-0.5 text-[10px] font-medium',
        props.connected
          ? 'border-success/30 bg-success/10 text-[var(--tone-success-text)]'
          : 'border-warning/30 bg-warning/10 text-[var(--tone-warning-text)]'
      )}
    >
      <span
        aria-hidden='true'
        className={cn(
          'size-1.5 rounded-full',
          props.connected
            ? 'bg-success motion-safe:animate-pulse'
            : 'bg-warning'
        )}
      />
      {props.connected ? t('Live') : t('Polling')}
    </span>
  )
}

export interface LiveRoutingCardProps {
  range: LiveRoutingStatsRange
  lane?: string
  onLaneChange?: (lane: string) => void
  /**
   * 独立页用 `ConsolePage` 承担标题，卡片不再重复渲染 `h2`；Live 徽章、车道数
   * 与当前车道信息仍保留在卡片头部。默认 false 保持卡片自带标题。
   */
  hideTitle?: boolean
}

export function LiveRoutingCard(props: LiveRoutingCardProps) {
  const { t } = useTranslation()
  const containerRef = useRef<HTMLDivElement>(null)
  const hubRef = useRef<HTMLDivElement>(null)
  const laneRefs = useRef(new Map<string, HTMLButtonElement>())
  const memberRefs = useRef(new Map<string, HTMLDivElement>())

  const lanesQuery = useQuery({
    queryKey: [...LIVE_QUERY_PREFIX, 'lanes'],
    queryFn: listPBRLaneSummaries,
    staleTime: 60 * 1000,
    meta: { errorToast: false },
  })
  const statsQuery = useQuery({
    queryKey: [...LIVE_QUERY_PREFIX, 'lane-stats', props.range.key],
    queryFn: async () => {
      const res = await getPBRStats({
        granularity: props.range.granularity ?? 'hour',
        from: props.range.from,
        to: props.range.to,
        groupBy: 'lane',
      })
      return res.items
    },
    staleTime: 30 * 1000,
    refetchInterval: 60 * 1000,
    meta: { errorToast: false },
  })

  const lanes = useMemo(
    () => sortLanesForRail(lanesQuery.data ?? []),
    [lanesQuery.data]
  )
  const laneNames = useMemo(() => lanes.map((lane) => lane.name), [lanes])
  const statsByLane = useMemo(
    () => laneStatsMap(statsQuery.data ?? []),
    [statsQuery.data]
  )
  // `sseDriven` 比“连接已建立”更准确：SSE 连上但帧过期时仍按轮询展示。
  const { runtime, nowMs } = useLaneRuntime(laneNames)

  const selectedLane = useMemo(() => {
    if (props.lane && lanes.some((lane) => lane.name === props.lane)) {
      return props.lane
    }
    return pickDefaultLane(lanes, statsByLane) ?? ''
  }, [props.lane, lanes, statsByLane])
  const selectedSummary: PBRLaneSummary | undefined = lanes.find(
    (lane) => lane.name === selectedLane
  )
  const snapshot = runtime.byLane.get(selectedLane)
  const rows = useMemo(
    () =>
      selectedSummary
        ? buildMemberRows({
            lane: selectedSummary,
            snapshot,
            now: nowMs,
          })
        : [],
    [selectedSummary, snapshot, nowMs]
  )
  const accents = useMemo(
    () =>
      new Map(lanes.map((lane, index) => [lane.name, getChartColor(index)])),
    [lanes]
  )
  const currentMemberKey = rows.find((row) => row.state.current)?.key
  // 被绕开的成员：冷却/熔断/连拒/不可用。它们的连线画成 rest（更疏、半透明），
  // 与「当前成员那根流动线」形成对比，让人一眼看到流量在走哪条腿。
  const restingMemberKeys = useMemo(
    () =>
      rows
        .filter((row) =>
          [
            'cooldown',
            'rate-limited',
            'circuit-open',
            'rejected',
            'unavailable',
          ].includes(row.state.kind)
        )
        .map((row) => row.key),
    [rows]
  )
  // 车道此刻是否真的有流量：SSE 有当前成员且它在服务（非冷却/熔断/连拒）。
  const laneBusy = Boolean(
    currentMemberKey &&
    rows.some(
      (row) =>
        row.key === currentMemberKey &&
        (row.state.kind === 'current' || row.state.kind === 'ready')
    )
  )
  const laneAccent = (lane: string) => accents.get(lane) ?? getChartColor(0)

  const laneConfigQuery = useQuery({
    queryKey: [...LIVE_QUERY_PREFIX, 'lane-config', selectedLane],
    queryFn: () => getPBRLane(selectedLane),
    enabled: selectedLane !== '',
    staleTime: 2 * 60 * 1000,
    meta: { errorToast: false },
  })
  const recentQuery = useQuery({
    queryKey: [...LIVE_QUERY_PREFIX, 'recent', selectedLane],
    queryFn: async () => {
      const res = await listPBRRequestLogs({ lane: selectedLane, limit: 1 })
      return res.items[0]
    },
    enabled: selectedLane !== '',
    refetchInterval: 15 * 1000,
    meta: { errorToast: false },
  })

  const policy = resolveLanePolicy(
    selectedSummary?.mode ?? laneConfigQuery.data?.mode,
    laneConfigQuery.data?.config
  )
  const firstMember = firstAvailableMember(snapshot)
  const stats = statsByLane.get(selectedLane) ?? EMPTY_STATS
  const modeLabel = policy.mode === 'manual' ? t('Manual') : t('Failover')

  let policyText = ''
  if (selectedSummary) {
    if (selectedSummary.enabled === false) {
      policyText = t('Lane disabled: this lane is not accepting traffic.')
    } else if (policy.mode === 'manual') {
      policyText = t(
        'Manual policy: always uses the pinned member. If it is unavailable, traffic is not rerouted.'
      )
    } else {
      const base = t(
        'Failover policy: tries members by priority, cools a failed member for {{cooldown}}s, and attempts up to {{attempts}} members per request.',
        {
          cooldown: policy.cooldownSeconds,
          attempts: policy.maxAttempts,
        }
      )
      policyText =
        policy.affinitySeconds > 0
          ? `${base} ${t(
              'Affinity policy: keeps a conversation on the last successful member for {{seconds}}s.',
              { seconds: policy.affinitySeconds }
            )}`
          : base
    }
  }

  const registerLaneRef = (lane: string, el: HTMLButtonElement | null) => {
    if (el) laneRefs.current.set(lane, el)
    else laneRefs.current.delete(lane)
  }
  const registerMemberRef = (key: string, el: HTMLDivElement | null) => {
    if (el) memberRefs.current.set(key, el)
    else memberRefs.current.delete(key)
  }
  const refresh = () => {
    void lanesQuery.refetch()
    void statsQuery.refetch()
    if (selectedLane) {
      void laneConfigQuery.refetch()
      void recentQuery.refetch()
    }
  }

  return (
    <section
      aria-label={t('Live routing')}
      className='border-border/70 bg-card shadow-control overflow-hidden rounded-xl border'
    >
      <header className='border-border/60 flex flex-col gap-2 border-b px-4 py-3'>
        <div className='flex flex-wrap items-center gap-2'>
          <Waypoints className='text-primary size-4' aria-hidden='true' />
          {!props.hideTitle && (
            <h2 className='text-sm font-semibold'>{t('Live routing')}</h2>
          )}
          <LiveBadge connected={runtime.sseDriven} />
          {lanes.length > 0 && (
            <span className='text-muted-foreground text-[11px]'>
              {lanes.length} {t('Lanes')}
            </span>
          )}
          <span className='ml-auto' />
          {selectedSummary && (
            <span className='text-muted-foreground inline-flex items-center gap-1.5 text-[11px]'>
              <span className='text-foreground font-mono'>
                {selectedSummary.name}
              </span>
              <span className='border-border rounded border px-1 py-0.5'>
                {modeLabel}
              </span>
            </span>
          )}
        </div>
        {policyText && (
          <p className='text-muted-foreground max-w-5xl text-xs leading-relaxed'>
            {policyText}
          </p>
        )}
      </header>

      <div className='px-4 py-4'>
        {lanesQuery.isPending && <LiveRoutingSkeleton />}

        {lanesQuery.isError && (
          <div className='border-destructive/30 bg-destructive/5 flex flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2'>
            <span className='text-xs'>{t('Live routing is unavailable')}</span>
            <Button type='button' size='sm' variant='outline' onClick={refresh}>
              <RefreshCw className='size-3.5' />
              {t('Retry')}
            </Button>
          </div>
        )}

        {lanesQuery.isSuccess && lanes.length === 0 && (
          <div className='flex flex-col items-center gap-2 py-8 text-center'>
            <Waypoints className='text-muted-foreground size-6' />
            <p className='text-sm font-medium'>{t('No lanes yet')}</p>
            <Link
              to='/routes'
              className={buttonVariants({ variant: 'outline', size: 'sm' })}
            >
              {t('Open routing')}
            </Link>
          </div>
        )}

        {lanes.length > 0 && (
          <div
            ref={containerRef}
            className='relative grid gap-4 lg:grid-cols-[210px_120px_minmax(0,1fr)] lg:items-start lg:gap-8'
          >
            <div className='relative z-10 min-w-0'>
              <LaneRail
                lanes={lanes}
                selectedLane={selectedLane}
                snapshots={runtime.byLane}
                accents={accents}
                onSelect={(lane) => props.onLaneChange?.(lane)}
                registerRef={registerLaneRef}
              />
            </div>

            <div className='relative z-10 hidden items-center justify-center self-stretch lg:flex'>
              <div
                ref={hubRef}
                className='border-border/70 bg-card relative flex flex-col items-center gap-1 rounded-2xl border px-4 py-3 shadow-sm'
              >
                <span
                  aria-hidden='true'
                  className='bg-success absolute top-2 right-2 size-1.5 rounded-full motion-safe:animate-pulse'
                />
                <span className='bg-primary/10 text-primary flex size-10 items-center justify-center rounded-xl'>
                  <Logo className='size-6' />
                </span>
                <span className='text-[11px] font-medium'>PBR</span>
                <span className='text-muted-foreground text-[10px]'>
                  {t('Gateway')}
                </span>
              </div>
            </div>

            <div className='relative z-10 min-w-0'>
              <div className='mb-2 flex items-center justify-between gap-2'>
                <span className='text-muted-foreground text-[11px]'>
                  {rows.length} {t('Members')}
                </span>
                <span className='text-muted-foreground text-[11px]'>
                  {runtime.sseDriven
                    ? t('Live updates via SSE')
                    : t('Falling back to polling')}
                </span>
              </div>
              <MemberRows
                rows={rows}
                accent={laneAccent(selectedLane)}
                registerRef={registerMemberRef}
              />
            </div>

            <RoutingConnectors
              containerRef={containerRef}
              hubRef={hubRef}
              laneRefs={laneRefs}
              memberRefs={memberRefs}
              laneKeys={lanes.map((lane) => lane.name)}
              memberKeys={rows.map((row) => row.key)}
              selectedLane={selectedLane}
              currentMemberKey={currentMemberKey}
              restingMemberKeys={restingMemberKeys}
              laneBusy={laneBusy}
              laneAccent={laneAccent}
            />
          </div>
        )}
      </div>

      {lanes.length > 0 && (
        <div className='px-4 pb-4'>
          <LiveRoutingFooter
            laneEnabled={selectedSummary?.enabled !== false}
            firstMember={firstMember}
            stats={stats}
            log={recentQuery.data}
            logPending={recentQuery.isPending}
          />
        </div>
      )}
    </section>
  )
}
