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
PowerBarRations —— 「路由与故障切换」独立页（ui-spec §6.3、ADR 0006）

以 **octopus 式卡片网格**列出全部路由键（已配车道 + 未配车道）：每张卡片展示路由键、
状态徽章、成员顺序与运行态；操作 = 编辑成员链 / 删除车道。页头「新建车道」进入两栏
编排器；未配车道的路由键点开即可组链。渠道声明/新增模型不会自动建车道。
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, Waypoints } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'

import {
  cleanupPBROrphanMembers,
  deletePBRFailover,
  listPBRLaneSummaries,
  listPBRModels,
  pbrModelsQueryKey,
  type PBRModelSummary,
} from './api'
import { LaneCard } from './components/lane-card'
import { LaneEditorDialog } from './components/lane-editor-dialog'
import { useLaneRuntime } from './hooks/use-lane-runtime'

// 车道顺序摘要挂在同一前缀 queryKey 下：编排器保存/删除后 invalidate 会一并刷新。
const laneSummariesKey = [...pbrModelsQueryKey, 'lane-summaries'] as const

export function Routes() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [editorOpen, setEditorOpen] = useState(false)
  const [editorModel, setEditorModel] = useState<string | undefined>(undefined)
  const [pendingDelete, setPendingDelete] = useState<PBRModelSummary | null>(
    null
  )
  const [cleanupOpen, setCleanupOpen] = useState(false)

  const modelsQuery = useQuery({
    queryKey: pbrModelsQueryKey,
    queryFn: listPBRModels,
  })
  const lanesQuery = useQuery({
    queryKey: laneSummariesKey,
    queryFn: listPBRLaneSummaries,
  })

  const models = modelsQuery.data ?? []
  // 车道运行态（ui-spec §4）：SSE 主源 + 30s 轮询兜底；只对已配车道的路由键订阅。
  const laneNames = models
    .filter((m) => m.source === 'explicit')
    .map((m) => m.model)
  const { runtime: laneRuntime, nowMs } = useLaneRuntime(laneNames)
  const laneOrders = new Map(
    (lanesQuery.data ?? []).map((lane) => [lane.name, lane.members])
  )
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

  const removeLane = useMutation({
    mutationFn: (model: string) => deletePBRFailover(model),
    onSuccess: async () => {
      toast.success(
        t('Lane removed; configure this model again to make it callable')
      )
      setPendingDelete(null)
      await queryClient.invalidateQueries({ queryKey: pbrModelsQueryKey })
      await queryClient.invalidateQueries({ queryKey: laneSummariesKey })
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : String(error))
    },
  })

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: pbrModelsQueryKey })
    await queryClient.invalidateQueries({ queryKey: laneSummariesKey })
  }

  const openNewLane = () => {
    setEditorModel(undefined)
    setEditorOpen(true)
  }
  const openEditLane = (model: string) => {
    setEditorModel(model)
    setEditorOpen(true)
  }

  let content
  if (modelsQuery.isLoading) {
    content = (
      <div className='text-muted-foreground flex items-center gap-2 p-3 text-sm'>
        <Loader2 className='size-4 animate-spin' /> {t('Loading...')}
      </div>
    )
  } else if (modelsQuery.isError) {
    content = (
      <ErrorState
        description={
          modelsQuery.error instanceof Error
            ? modelsQuery.error.message
            : String(modelsQuery.error)
        }
        onRetry={() => void modelsQuery.refetch()}
      />
    )
  } else if (models.length === 0) {
    content = (
      <EmptyState
        icon={Waypoints}
        title={t('No route keys yet')}
        description={t(
          'Create a lane by hand and pick members from any channel to make a route key callable.'
        )}
      />
    )
  } else {
    content = (
      <div className='grid grid-cols-1 gap-3 pb-4 md:grid-cols-2 xl:grid-cols-3'>
        {models.map((row) => (
          <LaneCard
            key={row.model}
            summary={row}
            members={laneOrders.get(row.model) ?? []}
            snapshot={laneRuntime.byLane.get(row.model)}
            now={nowMs}
            onEdit={() => openEditLane(row.model)}
            onDelete={() => setPendingDelete(row)}
          />
        ))}
      </div>
    )
  }

  return (
    <>
      <SectionPageLayout fixedContent stackActionsOnMobile>
        <SectionPageLayout.Title>
          {t('Routing & Failover')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button size='sm' onClick={openNewLane}>
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
            <div className='min-h-0 flex-1 overflow-auto'>{content}</div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      {editorOpen && (
        <LaneEditorDialog
          open
          onOpenChange={setEditorOpen}
          model={editorModel}
          onSaved={refresh}
        />
      )}

      <ConfirmDialog
        destructive
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
        title={t('Remove this lane?')}
        desc={t(
          'Removing the lane makes {{model}} unavailable immediately (requests return 503) until you configure a lane again.',
          { model: pendingDelete?.model ?? '' }
        )}
        confirmText={t('Remove lane')}
        isLoading={removeLane.isPending}
        handleConfirm={() => {
          if (pendingDelete) removeLane.mutate(pendingDelete.model)
        }}
      />

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
