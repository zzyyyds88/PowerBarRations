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
PowerBarRations —— 「路由与故障切换」独立页（ui-spec §6.3）

集中列出全部路由键（已配车道 + 未配车道，GET /api/v1/models）：每行展示模型名、
状态（explicit 可调用 / unconfigured 不可调用）、成员数与顺序摘要；行内
「编辑成员链」打开该模型的成员链抽屉。模型管理页专注模型目录，行内路由入口已移除。
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { GitBranch, Loader2, Plus, Waypoints } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import {
  StaticDataTable,
  staticDataTableClassNames as tableStyles,
} from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'

import {
  cleanupPBROrphanMembers,
  listPBRLaneSummaries,
  listPBRModels,
  pbrModelsQueryKey,
  type PBRModelSummary,
} from './api'
import { LaneRuntimeCell } from './components/lane-runtime-cell'
import { ModelRoutingDrawer } from './components/model-routing-drawer'
import { NewLaneDialog } from './components/new-lane-dialog'
import { useLaneRuntime } from './hooks/use-lane-runtime'

// 车道顺序摘要挂在同一前缀 queryKey 下：抽屉里保存/删除成员链后，
// 面板 invalidate ['pbr-routable-models'] 会连同本键一起刷新。
const laneSummariesKey = [...pbrModelsQueryKey, 'lane-summaries'] as const

export function Routes() {
  const { t } = useTranslation()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [currentRow, setCurrentRow] = useState<{ model_name: string } | null>(
    null
  )

  const modelsQuery = useQuery({
    queryKey: pbrModelsQueryKey,
    queryFn: listPBRModels,
  })
  const lanesQuery = useQuery({
    queryKey: laneSummariesKey,
    queryFn: listPBRLaneSummaries,
  })

  const queryClient = useQueryClient()
  const [cleanupOpen, setCleanupOpen] = useState(false)
  const [newLaneOpen, setNewLaneOpen] = useState(false)

  const models = modelsQuery.data ?? []
  // 车道运行态（ui-spec §4）：SSE 主源 + 30s 轮询兜底；只对已配车道的路由键订阅。
  const laneNames = models
    .filter((m) => m.source === 'explicit')
    .map((m) => m.model)
  const { runtime: laneRuntime, nowMs } = useLaneRuntime(laneNames)
  // 车道名 = 模型名：用车道列表补出每行的成员顺序（GET /api/v1/models 只有数量）。
  const laneOrders = new Map(
    (lanesQuery.data ?? []).map((lane) => [lane.name, lane.members])
  )
  // 悬空成员：渠道已删除、车道仍留着该成员（历史数据），提供一键清理。
  const orphanLanes = (lanesQuery.data ?? []).filter(
    (lane) => lane.orphan_member_count > 0
  )
  const cleanup = useMutation({
    mutationFn: cleanupPBROrphanMembers,
    onSuccess: async (result) => {
      toast.success(
        t('Cleaned {{cleaned}} lane(s); deleted {{deleted}} empty lane(s).', {
          cleaned: result.cleaned_lanes.length,
          deleted: result.deleted_lanes.length,
        })
      )
      setCleanupOpen(false)
      await queryClient.invalidateQueries({ queryKey: pbrModelsQueryKey })
    },
  })

  const openEditor = (row: PBRModelSummary) => {
    setCurrentRow({ model_name: row.model })
    setDrawerOpen(true)
  }

  let tableContent: ReactNode
  if (modelsQuery.isLoading) {
    tableContent = (
      <div className='text-muted-foreground flex items-center gap-2 p-3 text-sm'>
        <Loader2 className='size-4 animate-spin' /> {t('Loading...')}
      </div>
    )
  } else if (modelsQuery.isError) {
    tableContent = (
      <ErrorState
        description={
          modelsQuery.error instanceof Error
            ? modelsQuery.error.message
            : String(modelsQuery.error)
        }
        onRetry={() => void modelsQuery.refetch()}
      />
    )
  } else {
    tableContent = (
      <StaticDataTable
        className={tableStyles.sectionContainer}
        headerRowClassName={tableStyles.mutedHeaderRow}
        data={models}
        getRowKey={(row) => row.model}
        getRowClassName={() => 'hover:bg-muted/20'}
        emptyContent={
          <EmptyState
            icon={Waypoints}
            title={t('No route keys yet')}
            description={t(
              'Declare models on channels to see them here, then add members and save to create a lane.'
            )}
          />
        }
        columns={[
          {
            id: 'model',
            header: t('Model'),
            className: 'h-9 w-[min(360px,40%)]',
            cellClassName: tableStyles.topCell,
            cell: (row) => (
              <div className='flex min-w-0 items-center gap-1'>
                <span
                  className='truncate font-mono text-sm font-medium'
                  title={row.model}
                >
                  {row.model}
                </span>
                <CopyButton value={row.model} className='size-6 shrink-0' />
              </div>
            ),
          },
          {
            id: 'status',
            header: t('Status'),
            className: 'h-9 w-32',
            cellClassName: tableStyles.topCell,
            cell: (row) => {
              if (row.source === 'explicit') {
                // 车道存在 ≠ 现在可用：成员全冷却/熔断时给出"全部不可用"（routing-spec §7）。
                if (row.degraded) {
                  return (
                    <StatusBadge
                      label={t('All members unavailable')}
                      variant='danger'
                      size='sm'
                    />
                  )
                }
                const partiallyDegraded =
                  typeof row.healthy_member_count === 'number' &&
                  typeof row.health_member_count === 'number' &&
                  row.health_member_count > 0 &&
                  row.healthy_member_count < row.health_member_count
                if (partiallyDegraded) {
                  return (
                    <StatusBadge
                      label={t('Degraded')}
                      variant='warning'
                      size='sm'
                    />
                  )
                }
                return (
                  <StatusBadge
                    label={t('Callable')}
                    variant='success'
                    size='sm'
                  />
                )
              }
              if (row.source === 'disabled') {
                return (
                  <StatusBadge
                    label={t('Lane disabled')}
                    variant='warning'
                    size='sm'
                  />
                )
              }
              return (
                <StatusBadge
                  label={t('Not callable')}
                  variant='danger'
                  size='sm'
                />
              )
            },
          },
          {
            id: 'members',
            header: t('Members & order'),
            className: 'h-9 min-w-0',
            cellClassName: tableStyles.topCell,
            cell: (row) => {
              if (row.source !== 'explicit') {
                return (
                  <span className='text-muted-foreground text-xs'>
                    {t('{{count}} candidate channels', {
                      count: row.member_count,
                    })}
                  </span>
                )
              }
              const members = laneOrders.get(row.model) ?? []
              // 成员总数可能含渠道已删/停用的悬空成员，可用数才是真正可路由的（P3-1）。
              const unavailable =
                row.member_count - (row.available_member_count ?? 0)
              return (
                <div className='min-w-0'>
                  <div className='text-sm'>
                    {t('{{count}} members', { count: row.member_count })}
                    {unavailable > 0 && (
                      <span className='text-destructive ml-1 text-xs'>
                        {t('({{count}} unavailable)', { count: unavailable })}
                      </span>
                    )}
                  </div>
                  {members.length > 0 && (
                    <div
                      className='text-muted-foreground truncate text-xs'
                      title={members.map((m) => m.channel).join(' → ')}
                    >
                      {members.map((m) => m.channel).join(' → ')}
                    </div>
                  )}
                </div>
              )
            },
          },
          {
            id: 'runtime',
            header: t('Runtime'),
            className: 'h-9 w-56',
            cellClassName: tableStyles.topCell,
            cell: (row) =>
              row.source === 'explicit' ? (
                <LaneRuntimeCell
                  snapshot={laneRuntime.byLane.get(row.model)}
                  now={nowMs}
                />
              ) : (
                <span className='text-muted-foreground text-xs'>—</span>
              ),
          },
          {
            id: 'actions',
            header: t('Actions'),
            className: `h-9 w-36 ${tableStyles.actionHeaderCell}`,
            cellClassName: tableStyles.actionCell,
            cell: (row) => (
              <Button
                size='sm'
                variant='outline'
                onClick={() => openEditor(row)}
              >
                <GitBranch className='size-4' />
                {t('Edit members')}
              </Button>
            ),
          },
        ]}
      />
    )
  }

  return (
    <>
      <SectionPageLayout fixedContent stackActionsOnMobile>
        <SectionPageLayout.Title>
          {t('Routing & Failover')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button size='sm' onClick={() => setNewLaneOpen(true)}>
            <Plus className='size-4' />
            {t('New lane')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex h-full min-h-0 flex-col'>
            {lanesQuery.isError && (
              <div className='border-destructive/40 bg-destructive/5 text-destructive mb-3 flex items-center justify-between gap-2 rounded-md border px-3 py-2 text-xs'>
                <span>{t('Failed to load lane member order')}</span>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => void lanesQuery.refetch()}
                >
                  {t('Retry')}
                </Button>
              </div>
            )}
            {orphanLanes.length > 0 && (
              <div className='mb-3 flex items-center justify-between gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-400'>
                <span>
                  {t(
                    '{{count}} lane(s) have members whose channel no longer exists.',
                    { count: orphanLanes.length }
                  )}
                </span>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => setCleanupOpen(true)}
                >
                  {t('Clean up orphan members')}
                </Button>
              </div>
            )}
            <div className='min-h-0 flex-1 overflow-auto pb-4'>
              {tableContent}
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <ModelRoutingDrawer
        open={drawerOpen}
        onOpenChange={setDrawerOpen}
        currentRow={currentRow}
      />

      <NewLaneDialog open={newLaneOpen} onOpenChange={setNewLaneOpen} />

      <ConfirmDialog
        open={cleanupOpen}
        onOpenChange={setCleanupOpen}
        title={t('Clean up orphan members?')}
        desc={t(
          'Members whose channel no longer exists are removed from their lanes; lanes left without members are deleted. Those models stop being callable.'
        )}
        confirmText={t('Clean up')}
        isLoading={cleanup.isPending}
        handleConfirm={() => cleanup.mutate()}
      >
        <ul className='max-h-48 list-disc overflow-auto pl-5 text-xs'>
          {orphanLanes.map((lane) => (
            <li key={lane.name} className='font-mono'>
              {lane.name} ({lane.orphan_member_count})
            </li>
          ))}
        </ul>
      </ConfirmDialog>
    </>
  )
}
