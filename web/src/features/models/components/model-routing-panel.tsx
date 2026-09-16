/*
PowerBarRations —— 模型成员链（故障切换）面板

用户心智：渠道里填好上游与模型后，在**模型管理**里为每个模型定"优先打谁、再打谁"。
成员顺序即故障切换顺序：**顺序就是优先级**，界面不暴露 priority 数字输入，保存时按
数组位置生成 priority（首位最大）。只保留 failover/manual 两种模式（weighted /
round_robin 已删除）。保存即把成员链固化为显式 failover 车道
（PUT /api/v1/lanes/{model}）；未配车道时成员列表为空，添加成员后保存即固化。
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowUp,
  Loader2,
  Plus,
  Save,
  Trash2,
  Wand2,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'

import {
  deletePBRFailover,
  getPBRRoute,
  listPBRModels,
  savePBRFailover,
  seedPBRLanes,
  type PBRModelSummary,
} from '../pbr-routing-api'

const modelsKey = ['pbr-routable-models'] as const
const routeKey = (model: string) => ['pbr-route', model] as const

export function ModelRoutingPanel(props: { initialModel?: string }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState<string>(props.initialModel ?? '')

  const modelsQuery = useQuery({ queryKey: modelsKey, queryFn: listPBRModels })
  const models: PBRModelSummary[] = modelsQuery.data ?? []
  const knownModels = models.map((m) => m.model)
  const active =
    selected && knownModels.includes(selected)
      ? selected
      : selected || models[0]?.model || ''

  // 一键固化：为所有"渠道已声明但无车道"的模型生成 failover 车道（ADR 0005）。
  const seed = useMutation({
    mutationFn: () => seedPBRLanes(false),
    onSuccess: async (result) => {
      toast.success(
        t('Generated {{count}} lanes', { count: result.created.length })
      )
      await queryClient.invalidateQueries({ queryKey: modelsKey })
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : String(error))
    },
  })

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
              onClick={() => setSelected(m.model)}
              className={cn(
                'hover:bg-accent w-full rounded-md px-2 py-1.5 text-left text-sm transition-colors',
                active === m.model && 'bg-accent font-medium'
              )}
            >
              <span className='block truncate'>{m.model}</span>
              <span className='text-muted-foreground text-xs'>
                {m.routable ? t('Lane configured') : t('No lane · not callable')}{' '}
                · {t('{{count}} members', { count: m.member_count })}
              </span>
            </button>
          </li>
        ))}
      </ul>
    )
  }

  return (
    <div className='grid min-h-0 flex-1 gap-4 lg:grid-cols-[320px_1fr]'>
      <Card className='min-h-0 overflow-hidden'>
        <CardHeader className='flex-row items-center justify-between gap-2 py-3'>
          <CardTitle className='text-sm'>
            {t('Routable models')} ({models.length})
          </CardTitle>
          <Button
            size='sm'
            variant='outline'
            disabled={seed.isPending}
            onClick={() => seed.mutate()}
          >
            <Wand2 className='size-4' />
            {t('Generate missing lanes')}
          </Button>
        </CardHeader>
        <Separator />
        <CardContent className='min-h-0 overflow-auto p-2'>
          {listContent}
        </CardContent>
      </Card>

      {active ? (
        <RouteEditor
          key={active}
          model={active}
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
}: {
  model: string
  onSaved: () => Promise<void> | void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<EditableMember[] | null>(null)

  const routeQuery = useQuery({
    queryKey: routeKey(model),
    queryFn: () => getPBRRoute(model),
  })

  const routable = routeQuery.data?.routable !== false
  // 已配车道 → 用真实成员；未配车道 → 从空链开始（建议链只作候选）。
  const sourceMembers: EditableMember[] = routable
    ? (routeQuery.data?.members ?? []).map((m) => ({
        channel: m.channel,
        upstream_model: m.upstream_model,
        upstream_override: m.upstream_override ?? '',
        priority: m.priority,
      }))
    : []
  // 未配车道时，建议链（带 channel_id）作为"可添加成员"候选。
  const candidates = (routeQuery.data?.members ?? []).filter(
    (m) => !sourceMembers.some((s) => s.channel === m.channel)
  )

  const members: EditableMember[] = [...(draft ?? sourceMembers)]

  // 顺序即优先级：按列表位置重排，第一个最大。
  const reorder = (next: EditableMember[]) => {
    setDraft(next.map((m, index) => ({ ...m, priority: next.length - index })))
  }

  const move = (index: number, delta: number) => {
    const target = index + delta
    if (target < 0 || target >= members.length) return
    const next = [...members]
    const [item] = next.splice(index, 1)
    next.splice(target, 0, item)
    reorder(next)
  }

  const addMember = (candidate: { channel: string; upstream_model: string }) => {
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
    setDraft(next)
  }

  const emptyDraft = members.length === 0

  const save = useMutation({
    mutationFn: () =>
      savePBRFailover(
        model,
        members.map((m) => ({
          channel: m.channel,
          upstream_model: m.upstream_override,
          priority: m.priority,
        }))
      ),
    onSuccess: async () => {
      toast.success(t('Failover order saved'))
      setDraft(null)
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
      setDraft(null)
      await onSaved()
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : String(error))
    },
  })

  const dirty = draft !== null

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
        {!routable && (
          <p className='rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-700 dark:text-amber-400'>
            {t(
              'No lane configured yet — add members and save to make this model callable.'
            )}
          </p>
        )}
        <p className='text-muted-foreground text-xs'>
          {t(
            'Member order is the failover order: requests try the top member first and escape to the next on failure.'
          )}
        </p>

        {emptyDraft ? (
          <EmptyState
            title={t('No members yet')}
            description={t(
              'Add a member from the candidates below, then save to create the lane.'
            )}
          />
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

  return (
    <Card className='min-h-0 overflow-hidden'>
      <CardHeader className='flex-row items-center justify-between gap-3 py-3'>
        <CardTitle className='text-sm'>
          {t('Failover order for')} <code className='font-mono'>{model}</code>
          <span className='text-muted-foreground ml-2 text-xs font-normal'>
            {routable ? t('Lane configured') : t('No lane · not callable')}
          </span>
        </CardTitle>
        <div className='flex items-center gap-2'>
          <Button
            size='sm'
            variant='outline'
            disabled={clear.isPending}
            onClick={() => clear.mutate()}
          >
            <Trash2 className='size-4' />
            {t('Remove lane')}
          </Button>
          <Button
            size='sm'
            disabled={!dirty || emptyDraft || save.isPending}
            onClick={() => save.mutate()}
          >
            {save.isPending ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <Save className='size-4' />
            )}
            {t('Save')}
          </Button>
        </div>
      </CardHeader>
      <Separator />
      <CardContent className='min-h-0 overflow-auto p-3'>{body}</CardContent>
    </Card>
  )
}
