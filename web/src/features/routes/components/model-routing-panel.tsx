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
PowerBarRations —— 模型成员链（故障切换）面板（ui-spec §6.3）

用户心智：渠道里填好上游与模型后，在**路由与故障切换**页为每个模型定"优先打谁、
再打谁"。成员顺序即故障切换顺序：**顺序就是优先级**，界面不暴露 priority 数字输入，
保存时按数组位置生成 priority（首位最大）。只保留 failover/manual 两种模式。成员链
只支持**手工添加/删除**（一键固化入口已移除）。保存即把成员链固化为显式 failover
车道（PUT /api/v1/lanes/{model}）；未配车道时成员列表为空，添加成员后保存即固化。
删除车道是破坏性操作，需二次确认且会使该模型立即不可调用（503）。
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, Loader2, Plus, Save, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'

import {
  deletePBRFailover,
  getPBRRoute,
  listPBRModels,
  savePBRFailover,
  pbrModelsQueryKey,
  type PBRLaneMode,
  type PBRModelSummary,
} from '../api'

const modelsKey = pbrModelsQueryKey
const routeKey = (model: string) => ['pbr-route', model] as const

export function ModelRoutingPanel(props: {
  initialModel?: string
  /**
   * 固定模型模式（行内「编辑成员链」弹窗）：只渲染该模型的成员链编辑，
   * 隐藏模型选择器；不传时保持完整面板模式。
   */
  fixedModel?: string
  /** 草稿脏状态变化回调：供弹窗在关闭前拦截「放弃未保存修改？」。 */
  onDirtyChange?: (dirty: boolean) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState<string>(
    props.initialModel ?? props.fixedModel ?? ''
  )
  const [editorDirty, setEditorDirty] = useState(false)
  const [pendingModel, setPendingModel] = useState<string | null>(null)
  const fixed = props.fixedModel

  const modelsQuery = useQuery({
    queryKey: modelsKey,
    queryFn: listPBRModels,
    // 固定模型模式只编辑单条车道，无需拉取全模型列表。
    enabled: !fixed,
  })
  const models: PBRModelSummary[] = modelsQuery.data ?? []
  const knownModels = models.map((m) => m.model)
  let active: string
  if (fixed) {
    active = fixed
  } else if (selected && knownModels.includes(selected)) {
    active = selected
  } else {
    active = selected || models[0]?.model || ''
  }

  const handleDirtyChange = (dirty: boolean) => {
    setEditorDirty(dirty)
    props.onDirtyChange?.(dirty)
  }

  // 切换模型会让 RouteEditor 以新 key 重挂载并丢弃草稿：dirty 时先确认。
  const requestSelect = (model: string) => {
    if (model === active) return
    if (editorDirty) {
      setPendingModel(model)
      return
    }
    setSelected(model)
  }

  const confirmDiscardAndSwitch = () => {
    if (pendingModel) setSelected(pendingModel)
    setPendingModel(null)
    handleDirtyChange(false)
  }

  let listContent
  if (modelsQuery.isLoading) {
    listContent = (
      <div className='text-muted-foreground flex items-center gap-2 p-3 text-sm'>
        <Loader2 className='size-4 animate-spin' /> {t('Loading...')}
      </div>
    )
  } else if (models.length === 0) {
    listContent = (
      <EmptyState
        title={t('No routable models yet')}
        description={t(
          'Add a channel and declare its models, then the model appears here.'
        )}
      />
    )
  } else {
    listContent = (
      <ul className='space-y-1'>
        {models.map((m) => (
          <li key={m.model}>
            <button
              type='button'
              onClick={() => requestSelect(m.model)}
              className={cn(
                'hover:bg-accent w-full rounded-md px-2 py-1.5 text-left text-sm transition-colors',
                active === m.model && 'bg-accent font-medium'
              )}
            >
              <span className='block truncate'>{m.model}</span>
              <span className='text-muted-foreground text-xs'>
                {m.routable
                  ? t('Lane configured')
                  : t('No lane · not callable')}{' '}
                · {t('{{count}} members', { count: m.member_count })}
              </span>
            </button>
          </li>
        ))}
      </ul>
    )
  }

  return (
    <div
      className={cn(
        'min-h-0 flex-1 gap-4',
        fixed ? 'flex flex-col' : 'grid lg:grid-cols-[320px_1fr]'
      )}
    >
      {!fixed && (
        <Card className='min-h-0 overflow-hidden'>
          <CardHeader className='flex-row items-center justify-between gap-2 py-3'>
            <CardTitle className='text-sm'>
              {t('Routable models')} ({models.length})
            </CardTitle>
          </CardHeader>
          <Separator />
          <CardContent className='min-h-0 overflow-auto p-2'>
            {listContent}
          </CardContent>
        </Card>
      )}

      {active ? (
        <RouteEditor
          key={active}
          model={active}
          onDirtyChange={handleDirtyChange}
          onSaved={async () => {
            await queryClient.invalidateQueries({ queryKey: modelsKey })
            await queryClient.invalidateQueries({ queryKey: routeKey(active) })
          }}
        />
      ) : (
        <Card className='min-h-0'>
          <CardContent className='flex h-full items-center justify-center p-6'>
            <EmptyState
              title={t('Select a model')}
              description={t('Pick a model on the left to configure failover.')}
            />
          </CardContent>
        </Card>
      )}

      <ConfirmDialog
        open={pendingModel !== null}
        onOpenChange={(open) => {
          if (!open) setPendingModel(null)
        }}
        title={t('Discard unsaved changes?')}
        desc={t('Your changes have not been saved.')}
        confirmText={t('Discard changes')}
        handleConfirm={confirmDiscardAndSwitch}
      />
    </div>
  )
}

interface EditableMember {
  channel: string
  /** 解析后的上游真名（仅展示，添加时取渠道映射的默认值）。 */
  upstream_model: string
  /** 成员级显式改名原值；为空 = 用渠道映射。 */
  upstream_override: string
  /** 车道内顺序：数字大者优先；由数组位置生成，界面不直接编辑。 */
  priority: number
}

function RouteEditor({
  model,
  onSaved,
  onDirtyChange,
}: {
  model: string
  onSaved: () => Promise<void> | void
  onDirtyChange?: (dirty: boolean) => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<EditableMember[] | null>(null)
  const [removeConfirmOpen, setRemoveConfirmOpen] = useState(false)
  // 模式与 active member 也走草稿语义：改动即 dirty，关闭/切换时一并确认放弃。
  const [modeDraft, setModeDraft] = useState<PBRLaneMode | null>(null)
  const [activeDraft, setActiveDraft] = useState<string | null>(null)

  const routeQuery = useQuery({
    queryKey: routeKey(model),
    queryFn: () => getPBRRoute(model),
  })

  // 停用车道：车道存在且成员可编辑，但当前不可调用（source=disabled）。
  const laneDisabled = routeQuery.data?.source === 'disabled'
  const routable = routeQuery.data?.routable !== false && !laneDisabled
  // 已配车道 → 用真实成员；未配车道 → 从空链开始（建议链只作候选）。
  const sourceMembers: EditableMember[] = routable
    ? (routeQuery.data?.members ?? []).map((m) => ({
        channel: m.channel,
        upstream_model: m.upstream_model,
        upstream_override: m.upstream_override ?? '',
        priority: m.priority,
      }))
    : []
  // 可添加成员候选：
  // - 已配车道：后端在 candidates 里给"声明了该模型但不在成员链里"的渠道；
  // - 未配车道：members 本身就是渠道声明形成的建议链（去掉已在草稿里的）。
  const candidates = routable
    ? (routeQuery.data?.candidates ?? [])
    : (routeQuery.data?.members ?? []).filter(
        (m) => !sourceMembers.some((s) => s.channel === m.channel)
      )

  const members: EditableMember[] = [...(draft ?? sourceMembers)]
  const currentMode: PBRLaneMode =
    modeDraft ?? (routeQuery.data?.mode === 'manual' ? 'manual' : 'failover')
  const currentActive = activeDraft ?? routeQuery.data?.active_member ?? ''
  // manual 的 active member 取值：优先成员别名，其次 "channel/upstream_model" 标签
  // （与后端 activeMemberIndex 的匹配规则一致）。
  const memberOptions = members.map((m) => {
    const alias = routeQuery.data?.members.find(
      (x) => x.channel === m.channel
    )?.public_alias
    const value = alias ? alias : m.channel + '/' + m.upstream_model
    return { value, label: alias ? alias + ' (' + m.channel + ')' : value }
  })

  // 统一维护草稿并同步脏状态，避免切换/关闭时静默丢弃。
  const updateDraft = (next: EditableMember[] | null) => {
    setDraft(next)
    onDirtyChange?.(next !== null)
  }

  // 顺序即优先级：按列表位置重排，第一个最大。
  const reorder = (next: EditableMember[]) => {
    updateDraft(
      next.map((m, index) => ({ ...m, priority: next.length - index }))
    )
  }

  const move = (index: number, delta: number) => {
    const target = index + delta
    if (target < 0 || target >= members.length) return
    const next = [...members]
    const [item] = next.splice(index, 1)
    next.splice(target, 0, item)
    reorder(next)
  }

  const addMember = (candidate: {
    channel: string
    upstream_model: string
  }) => {
    if (members.some((m) => m.channel === candidate.channel)) return
    reorder([
      ...members,
      {
        channel: candidate.channel,
        upstream_model: candidate.upstream_model,
        upstream_override: '',
        priority: 1,
      },
    ])
  }

  const removeMember = (index: number) => {
    reorder(members.filter((_, i) => i !== index))
  }

  const renameUpstream = (index: number, value: string) => {
    const next = [...members]
    next[index] = { ...next[index], upstream_override: value }
    updateDraft(next)
  }

  const emptyDraft = members.length === 0
  // 单层判定，避免嵌套三元（AGENTS §3.2）。
  let laneStatusLabel = t('No lane · not callable')
  if (laneDisabled) {
    laneStatusLabel = t('Lane disabled · not callable')
  } else if (routable) {
    laneStatusLabel = t('Lane configured')
  }

  const save = useMutation({
    mutationFn: () =>
      savePBRFailover(
        model,
        members.map((m) => ({
          channel: m.channel,
          upstream_model: m.upstream_override,
          priority: m.priority,
        })),
        {
          mode: currentMode,
          activeMember: currentMode === 'manual' ? currentActive : '',
        }
      ),
    onSuccess: async () => {
      toast.success(t('Failover order saved'))
      updateDraft(null)
      setModeDraft(null)
      setActiveDraft(null)
      await onSaved()
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : String(error))
    },
  })

  const clear = useMutation({
    mutationFn: () => deletePBRFailover(model),
    onSuccess: async () => {
      toast.success(
        t('Lane removed; configure this model again to make it callable')
      )
      updateDraft(null)
      await onSaved()
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : String(error))
    },
  })

  const dirty = draft !== null || modeDraft !== null || activeDraft !== null
  // manual 模式必须指定 active member，否则车道无可选成员（后端 activeMemberIndex 返回 -1）。
  const manualMissingActive = currentMode === 'manual' && currentActive === ''

  let body
  if (routeQuery.isLoading) {
    body = (
      <div className='text-muted-foreground flex items-center gap-2 text-sm'>
        <Loader2 className='size-4 animate-spin' /> {t('Loading...')}
      </div>
    )
  } else {
    body = (
      <div className='space-y-3'>
        {laneDisabled && (
          <p className='rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-700 dark:text-amber-400'>
            {t(
              'This lane is disabled — the model is not callable. Save to enable it again.'
            )}
          </p>
        )}
        {!routable && !laneDisabled && (
          <p className='rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-700 dark:text-amber-400'>
            {t(
              'No lane configured yet — add members and save to make this model callable.'
            )}
          </p>
        )}
        <div className='flex flex-wrap items-center gap-3'>
          <div className='flex items-center gap-2'>
            <span className='text-xs font-medium'>{t('Mode')}</span>
            <Select
              value={currentMode}
              onValueChange={(value) => {
                setModeDraft(value === 'manual' ? 'manual' : 'failover')
                if (value !== 'manual') setActiveDraft('')
              }}
            >
              <SelectTrigger
                size='sm'
                aria-label={t('Mode')}
                className='w-[140px]'
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='failover'>{t('failover')}</SelectItem>
                <SelectItem value='manual'>{t('manual')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {currentMode === 'manual' && (
            <div className='flex items-center gap-2'>
              <span className='text-xs font-medium'>{t('Active member')}</span>
              <Select
                value={currentActive}
                onValueChange={(value) => setActiveDraft(value ?? '')}
              >
                <SelectTrigger
                  size='sm'
                  aria-label={t('Active member')}
                  className='w-[220px]'
                >
                  <SelectValue placeholder={t('Select a member')} />
                </SelectTrigger>
                <SelectContent>
                  {memberOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
        </div>
        {manualMissingActive && (
          <p className='text-destructive text-xs'>
            {t('Pick the active member for manual mode before saving.')}
          </p>
        )}
        <p className='text-muted-foreground text-xs'>
          {t(
            'Member order is the failover order: requests try the top member first and escape to the next on failure.'
          )}
        </p>

        {emptyDraft ? (
          <>
            <EmptyState
              title={t('No members yet')}
              description={t(
                'Add a member from the candidates below, then save to create the lane.'
              )}
            />
            <p className='text-muted-foreground text-xs'>
              {t('Keep at least one member, or remove the lane.')}
            </p>
          </>
        ) : (
          <div className='space-y-2'>
            {members.map((m, index) => (
              <div
                key={m.channel}
                className='flex items-center gap-2 rounded-md border p-2'
              >
                <span className='text-muted-foreground w-6 text-center text-xs'>
                  {index + 1}
                </span>
                <div className='min-w-0 flex-1'>
                  <div className='truncate text-sm font-medium'>
                    {m.channel}
                  </div>
                  <div className='text-muted-foreground truncate text-xs'>
                    {t('Resolved upstream')}: {m.upstream_model}
                  </div>
                </div>
                <Input
                  className='h-8 w-44'
                  aria-label={t('Upstream model for {{channel}}', {
                    channel: m.channel,
                  })}
                  placeholder={t('Use channel mapping')}
                  value={m.upstream_override}
                  onChange={(event) =>
                    renameUpstream(index, event.target.value)
                  }
                />
                <Button
                  size='icon'
                  variant='ghost'
                  aria-label={t('Move up')}
                  disabled={index === 0}
                  onClick={() => move(index, -1)}
                >
                  <ArrowUp className='size-4' />
                </Button>
                <Button
                  size='icon'
                  variant='ghost'
                  aria-label={t('Move down')}
                  disabled={index === members.length - 1}
                  onClick={() => move(index, 1)}
                >
                  <ArrowDown className='size-4' />
                </Button>
                <Button
                  size='icon'
                  variant='ghost'
                  aria-label={t('Remove member')}
                  onClick={() => removeMember(index)}
                >
                  <Trash2 className='size-4' />
                </Button>
              </div>
            ))}
          </div>
        )}

        {candidates.length > 0 && (
          <div className='space-y-2 rounded-md border border-dashed p-2'>
            <p className='text-muted-foreground text-xs'>
              {t('Candidate channels (declared in channels)')}
            </p>
            <div className='flex flex-wrap gap-2'>
              {candidates.map((candidate) => (
                <Button
                  key={candidate.channel}
                  size='sm'
                  variant='outline'
                  onClick={() => addMember(candidate)}
                >
                  <Plus className='size-4' />
                  {candidate.channel}
                </Button>
              ))}
            </div>
          </div>
        )}
      </div>
    )
  }

  // 单层判定，避免嵌套三元（AGENTS §3.2）。
  let saveDisabledReason: string | undefined
  if (emptyDraft) {
    saveDisabledReason = t('Keep at least one member, or remove the lane.')
  } else if (manualMissingActive) {
    saveDisabledReason = t(
      'Pick the active member for manual mode before saving.'
    )
  }

  const saveButton = (
    <Button
      size='sm'
      disabled={
        (!dirty && !laneDisabled) ||
        emptyDraft ||
        manualMissingActive ||
        save.isPending
      }
      title={saveDisabledReason}
      onClick={() => save.mutate()}
    >
      {save.isPending ? (
        <Loader2 className='size-4 animate-spin' />
      ) : (
        <Save className='size-4' />
      )}
      {t('Save')}
    </Button>
  )

  return (
    <>
      <Card className='min-h-0 overflow-hidden'>
        <CardHeader className='flex-row items-center justify-between gap-3 py-3'>
          <CardTitle className='text-sm'>
            {t('Failover order for')} <code className='font-mono'>{model}</code>
            <span className='text-muted-foreground ml-2 text-xs font-normal'>
              {laneStatusLabel}
            </span>
          </CardTitle>
          <div className='flex items-center gap-2'>
            <Button
              size='sm'
              variant='outline'
              disabled={
                (!routable && !laneDisabled) ||
                routeQuery.isLoading ||
                clear.isPending
              }
              onClick={() => setRemoveConfirmOpen(true)}
            >
              <Trash2 className='size-4' />
              {t('Remove lane')}
            </Button>
            {saveButton}
          </div>
        </CardHeader>
        <Separator />
        <CardContent className='min-h-0 overflow-auto p-3'>{body}</CardContent>
      </Card>

      <ConfirmDialog
        destructive
        open={removeConfirmOpen}
        onOpenChange={setRemoveConfirmOpen}
        title={t('Remove this lane?')}
        desc={t(
          'Removing the lane makes {{model}} unavailable immediately (requests return 503) until you configure a lane again.',
          { model }
        )}
        confirmText={t('Remove lane')}
        isLoading={clear.isPending}
        handleConfirm={() => {
          setRemoveConfirmOpen(false)
          clear.mutate()
        }}
      />
    </>
  )
}
