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
车道成员编排器（ui-spec §6.3、ADR 0006）：新建车道与编辑成员链共用。

两栏布局（对齐线上 octopus 分组编辑器）：
- 左栏「添加成员」：从 GET /api/v1/channels 拉全部启用渠道的 models，按渠道折叠，
  带搜索；点某个模型即把「该渠道 × 该模型」加入右栏。**没有「自动添加」**。
- 右栏「成员（顺序）」：已选成员有序列表，上移/下移、删除、改上游真名、清空。

成员唯一键 = (渠道, 上游真名)：同一渠道可在一条车道内出现多次。路由键可任意命名，
无需任何渠道声明过它；候选为空时仍可保存（只要已选成员非空）。
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowUp,
  Loader2,
  Plus,
  Search,
  Trash2,
  X,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getLobeIcon } from '@/lib/lobe-icon'
import { resolveModelProvider } from '@/lib/model-provider'
import { cn } from '@/lib/utils'

import {
  BUILTIN_LANE_DEFAULTS,
  listPBRChannelCatalog,
  memberKey,
  savePBRFailover,
  type PBRLaneMode,
  type PBRChannelCatalogEntry,
} from '../api'

/** 编排器里的一个已选成员（草稿态）。 */
export interface ComposerMember {
  /**
   * 稳定标识：仅用于 React key / 拖拽身份。不能把 channel 或 upstreamOverride
   * 拼进 key——那样每次改名都会重挂载行、输入框失焦（实测 bug）。
   */
  id: string
  channel: string
  /** 成员级显式改名原值；空 = 用渠道映射。 */
  upstreamOverride: string
  /** 渠道映射/路由键解析出的上游真名，仅展示。 */
  resolvedUpstream: string
  /** 成员别名（可选，仅用于 manual 的 active_member 取值）。 */
  publicAlias?: string
}

export interface LaneComposerProps {
  /** 新建时为空；编辑既有车道时为该车道名（路由键只读）。 */
  model?: string
  /** 新建模式下的路由键初值（从"未配车道"卡片进入时预填，仍可编辑）。 */
  initialName?: string
  /** 打开时从后端载入的初始成员（编辑既有车道用）。 */
  initialMembers?: ComposerMember[]
  initialMode?: PBRLaneMode
  initialActiveMember?: string
  initialConfig?: Record<string, number> | null
  onDirtyChange?: (dirty: boolean) => void
  onSaved: () => Promise<void> | void
  onCancel: () => void
}

// 单调递增的成员 id：稳定、无随机性，测试可复现。
let memberIdSeq = 0
function nextMemberId(): string {
  memberIdSeq += 1
  return `m${memberIdSeq}`
}

/** 渠道映射/路由键解析出的展示用上游真名。 */
function resolveDisplayUpstream(
  routeKey: string,
  channelName: string,
  override: string,
  catalog: PBRChannelCatalogEntry[]
): string {
  const trimmed = override.trim()
  if (trimmed !== '' && trimmed !== routeKey) return trimmed
  const channel = catalog.find((c) => c.name === channelName)
  const mapped = channel?.model_mapping?.[routeKey]
  if (mapped && mapped.trim() !== '') return mapped.trim()
  return trimmed !== '' ? trimmed : routeKey
}

function ModelIcon(props: { name: string }) {
  const provider = resolveModelProvider(props.name)
  const icon = provider?.icon
  if (icon) return <>{getLobeIcon(icon, 16)}</>
  return (
    <span className='bg-muted text-muted-foreground flex size-4 items-center justify-center rounded text-[9px] font-medium uppercase'>
      {props.name.trim()[0] ?? '?'}
    </span>
  )
}

/** 左栏一个可选模型项；已加入时禁用（同一 (渠道, 模型) 不可重复）。 */
function ModelPickerItem(props: {
  model: string
  added: boolean
  onAdd: () => void
}) {
  return (
    <button
      type='button'
      disabled={props.added}
      onClick={props.onAdd}
      aria-label={props.model}
      title={props.model}
      className={cn(
        'flex w-full items-center gap-2 rounded-md border px-2 py-1.5 text-left text-xs transition-colors',
        props.added ? 'cursor-not-allowed opacity-50' : 'hover:bg-muted'
      )}
    >
      <ModelIcon name={props.model} />
      <span className='min-w-0 flex-1 truncate font-mono'>{props.model}</span>
      <Plus className='size-3.5 shrink-0' />
    </button>
  )
}

export function LaneComposer(props: LaneComposerProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const editing = Boolean(props.model)

  const [name, setName] = useState(props.model ?? props.initialName ?? '')
  const [members, setMembers] = useState<ComposerMember[]>(
    props.initialMembers ?? []
  )
  const [mode, setMode] = useState<PBRLaneMode>(props.initialMode ?? 'failover')
  const [activeMember, setActiveMember] = useState(
    props.initialActiveMember ?? ''
  )
  const [config, setConfig] = useState<Record<string, number>>(
    props.initialConfig ?? { ...BUILTIN_LANE_DEFAULTS }
  )
  const [search, setSearch] = useState('')
  const [advancedOpen, setAdvancedOpen] = useState(false)

  const catalogQuery = useQuery({
    queryKey: ['pbr-channel-catalog'],
    queryFn: listPBRChannelCatalog,
  })
  const catalog = useMemo(() => catalogQuery.data ?? [], [catalogQuery.data])
  const routeKey = props.model ?? name.trim()

  const markDirty = (next: ComposerMember[]) => {
    setMembers(next)
    props.onDirtyChange?.(true)
  }

  const selectedKeys = useMemo(
    () =>
      new Set(
        members.map((m) =>
          memberKey({
            channel: m.channel,
            upstream_model: m.upstreamOverride.trim() || m.resolvedUpstream,
          })
        )
      ),
    [members]
  )

  const keyword = search.trim().toLowerCase()
  const channels = useMemo(() => {
    return catalog
      .filter((c) => c.enabled && c.models.length > 0)
      .map((c) => ({
        ...c,
        models: keyword
          ? c.models.filter(
              (m) =>
                m.toLowerCase().includes(keyword) ||
                c.name.toLowerCase().includes(keyword)
            )
          : c.models,
      }))
      .filter((c) => c.models.length > 0)
  }, [catalog, keyword])

  const addMember = (channel: string, upstream: string) => {
    // 默认显式填所选模型名（ADR 0006）；按 (渠道, 上游真名) 去重。
    const key = memberKey({ channel, upstream_model: upstream })
    if (
      members.some(
        (m) =>
          memberKey({
            channel: m.channel,
            upstream_model: m.upstreamOverride.trim() || m.resolvedUpstream,
          }) === key
      )
    ) {
      return
    }
    markDirty([
      ...members,
      {
        id: nextMemberId(),
        channel,
        upstreamOverride: upstream,
        resolvedUpstream: resolveDisplayUpstream(
          routeKey,
          channel,
          upstream,
          catalog
        ),
      },
    ])
  }

  const removeMember = (index: number) => {
    markDirty(members.filter((_, i) => i !== index))
  }

  const moveMember = (index: number, delta: number) => {
    const target = index + delta
    if (target < 0 || target >= members.length) return
    const next = [...members]
    const [item] = next.splice(index, 1)
    next.splice(target, 0, item)
    markDirty(next)
  }

  const renameMember = (index: number, value: string) => {
    const next = [...members]
    next[index] = {
      ...next[index],
      upstreamOverride: value,
      resolvedUpstream: resolveDisplayUpstream(
        routeKey,
        next[index].channel,
        value,
        catalog
      ),
    }
    markDirty(next)
  }

  const updateConfig = (key: string, value: number) => {
    setConfig((prev) => ({ ...prev, [key]: value }))
  }

  const save = useMutation({
    mutationFn: async () => {
      await savePBRFailover(
        routeKey,
        members.map((m, index) => ({
          channel: m.channel,
          upstream_model: m.upstreamOverride.trim(),
          priority: members.length - index,
        })),
        {
          mode,
          activeMember: mode === 'manual' ? activeMember : '',
          config,
        }
      )
    },
    onSuccess: async () => {
      toast.success(t(editing ? 'Failover order saved' : 'Lane saved'))
      await queryClient.invalidateQueries({ queryKey: ['pbr-routable-models'] })
      await queryClient.invalidateQueries({ queryKey: ['pbr-route'] })
      await props.onSaved()
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : String(error))
    },
  })

  const trimmedKey = routeKey
  const emptyMembers = members.length === 0
  const manualMissingActive = mode === 'manual' && activeMember === ''
  const canSave =
    trimmedKey.length > 0 &&
    !emptyMembers &&
    !manualMissingActive &&
    !save.isPending

  let saveDisabledReason: string | undefined
  if (trimmedKey.length === 0) {
    saveDisabledReason = t('Enter a route key first.')
  } else if (emptyMembers) {
    saveDisabledReason = t('Keep at least one member, or remove the lane.')
  } else if (manualMissingActive) {
    saveDisabledReason = t(
      'Pick the active member for manual mode before saving.'
    )
  }

  // 左栏内容用单层判定构建，避免嵌套三元（web/AGENTS §3.2）。
  let leftPanelContent
  if (catalogQuery.isLoading) {
    leftPanelContent = (
      <p className='text-muted-foreground flex items-center gap-2 p-2 text-xs'>
        <Loader2 className='size-3.5 animate-spin' /> {t('Loading...')}
      </p>
    )
  } else if (channels.length === 0) {
    leftPanelContent = (
      <p className='text-muted-foreground p-2 text-xs'>
        {t('No channels declare models yet.')}
      </p>
    )
  } else {
    leftPanelContent = (
      <Accordion multiple className='space-y-1'>
        {channels.map((channel) => (
          <AccordionItem
            key={channel.name}
            value={channel.name}
            className='rounded-md border px-2'
          >
            <AccordionTrigger className='py-2 hover:no-underline'>
              <span className='truncate font-mono text-xs'>{channel.name}</span>
              <span className='text-muted-foreground ml-2 shrink-0 text-xs'>
                {channel.models.length}
              </span>
            </AccordionTrigger>
            <AccordionContent className='space-y-1 pt-1'>
              {channel.models.map((model) => (
                <ModelPickerItem
                  key={model}
                  model={model}
                  added={selectedKeys.has(
                    memberKey({ channel: channel.name, upstream_model: model })
                  )}
                  onAdd={() => addMember(channel.name, model)}
                />
              ))}
            </AccordionContent>
          </AccordionItem>
        ))}
      </Accordion>
    )
  }

  const memberOptions = members.map((m) => {
    const alias = m.publicAlias ?? ''
    const value = alias || `${m.channel}/${m.resolvedUpstream}`
    return {
      value,
      label: alias
        ? `${alias} (${m.channel})`
        : `${m.channel} / ${m.resolvedUpstream}`,
    }
  })
  // 触发框显示所选成员的标签；找不到匹配项时回退原始值（不静默显示空）。
  const activeMemberLabel =
    memberOptions.find((option) => option.value === activeMember)?.label ??
    activeMember

  return (
    <div className='flex min-h-0 flex-1 flex-col gap-4'>
      <div className='grid shrink-0 grid-cols-1 gap-3 md:grid-cols-2'>
        <div className='space-y-1.5'>
          <Label htmlFor='lane-route-key'>{t('Route key')}</Label>
          <Input
            id='lane-route-key'
            value={routeKey}
            readOnly={editing}
            disabled={editing}
            placeholder={t('e.g. my-pooled-model')}
            onChange={(event) => {
              setName(event.target.value)
              props.onDirtyChange?.(true)
            }}
          />
        </div>
        <div className='space-y-1.5'>
          <Label>{t('Mode')}</Label>
          <Select
            value={mode}
            onValueChange={(value) => {
              setMode(value === 'manual' ? 'manual' : 'failover')
              if (value !== 'manual') setActiveMember('')
              props.onDirtyChange?.(true)
            }}
          >
            <SelectTrigger aria-label={t('Mode')} className='w-full'>
              {/* 显式给 SelectValue 子节点：Base UI 默认回显原始值（failover），
                  会绕过 i18n，中文界面显示英文。 */}
              <SelectValue>{t(mode)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='failover'>{t('failover')}</SelectItem>
              <SelectItem value='manual'>{t('manual')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      {mode === 'manual' && (
        <div className='space-y-1.5'>
          <Label>{t('Active member')}</Label>
          <Select
            value={activeMember}
            onValueChange={(value) => {
              setActiveMember(value ?? '')
              props.onDirtyChange?.(true)
            }}
          >
            <SelectTrigger aria-label={t('Active member')} className='w-full'>
              <SelectValue placeholder={t('Select a member')}>
                {activeMemberLabel}
              </SelectValue>
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

      <Collapsible
        open={advancedOpen}
        onOpenChange={setAdvancedOpen}
        className='shrink-0 rounded-md border'
      >
        <CollapsibleTrigger className='hover:bg-muted/50 flex w-full items-center justify-between px-3 py-2 text-sm font-medium'>
          {t('Advanced')}
        </CollapsibleTrigger>
        <CollapsibleContent className='grid grid-cols-2 gap-2 border-t p-3 md:grid-cols-3'>
          {(
            [
              ['member_max_attempts', 'Max attempts'],
              ['member_retry_interval_seconds', 'Retry interval (s)'],
              [
                'member_non_stream_response_timeout_seconds',
                'Non-stream timeout (s)',
              ],
              [
                'member_stream_first_event_timeout_seconds',
                'Stream first event timeout (s)',
              ],
              ['member_cooldown_seconds', 'Cooldown (s)'],
              ['member_affinity_seconds', 'Affinity (s)'],
            ] as const
          ).map(([key, label]) => (
            <div key={key} className='space-y-1'>
              <Label className='text-muted-foreground text-xs'>
                {t(label)}
              </Label>
              <Input
                type='number'
                min={0}
                className='h-8'
                value={String(config[key] ?? BUILTIN_LANE_DEFAULTS[key])}
                onChange={(event) => {
                  const parsed = Number.parseInt(event.target.value, 10)
                  updateConfig(
                    key,
                    Number.isFinite(parsed) && parsed >= 0 ? parsed : 0
                  )
                  props.onDirtyChange?.(true)
                }}
              />
            </div>
          ))}
        </CollapsibleContent>
      </Collapsible>

      <div className='grid min-h-0 flex-1 grid-cols-1 gap-3 md:grid-cols-2'>
        {/* 左栏：渠道 → 模型选择器 */}
        <div className='flex min-h-0 flex-col rounded-md border'>
          <div className='flex shrink-0 items-center gap-2 border-b px-3 py-2'>
            <span className='text-sm font-medium'>{t('Add members')}</span>
            <div className='relative ml-auto w-40'>
              <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2' />
              <Input
                className='h-7 pl-7 text-xs'
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t('Search channels or models')}
                aria-label={t('Search channels or models')}
              />
            </div>
          </div>
          <div className='min-h-0 flex-1 overflow-y-auto p-2'>
            {leftPanelContent}
          </div>
        </div>

        {/* 右栏：已选成员（顺序） */}
        <div className='flex min-h-0 flex-col rounded-md border'>
          <div className='flex shrink-0 items-center justify-between border-b px-3 py-2'>
            <span className='text-sm font-medium'>
              {t('Members (order)')}
              {members.length > 0 && (
                <span className='text-muted-foreground ml-1 text-xs font-normal'>
                  ({members.length})
                </span>
              )}
            </span>
            <Button
              type='button'
              size='sm'
              variant='ghost'
              disabled={members.length === 0}
              onClick={() => markDirty([])}
            >
              <Trash2 className='size-3.5' />
              {t('Clear')}
            </Button>
          </div>
          <div className='min-h-0 flex-1 overflow-y-auto p-2'>
            {emptyMembers ? (
              <EmptyState
                title={t('No members yet')}
                description={t(
                  'Pick models from the left to add them to this lane.'
                )}
              />
            ) : (
              <div className='space-y-1.5'>
                {members.map((member, index) => (
                  <div
                    key={member.id}
                    className='flex items-center gap-2 rounded-md border p-2'
                  >
                    <span className='text-muted-foreground w-5 shrink-0 text-center text-xs'>
                      {index + 1}
                    </span>
                    <div className='min-w-0 flex-1'>
                      <div className='truncate font-mono text-xs font-medium'>
                        {member.channel}
                      </div>
                      <div className='text-muted-foreground truncate text-xs'>
                        {t('Resolved upstream')}: {member.resolvedUpstream}
                      </div>
                    </div>
                    <Input
                      className='h-7 w-32 shrink-0 text-xs'
                      aria-label={t('Upstream model for {{channel}}', {
                        channel: member.channel,
                      })}
                      placeholder={t('Use channel mapping')}
                      value={member.upstreamOverride}
                      onChange={(event) =>
                        renameMember(index, event.target.value)
                      }
                    />
                    <Button
                      type='button'
                      size='icon'
                      variant='ghost'
                      className='size-7 shrink-0'
                      aria-label={t('Move up')}
                      disabled={index === 0}
                      onClick={() => moveMember(index, -1)}
                    >
                      <ArrowUp className='size-3.5' />
                    </Button>
                    <Button
                      type='button'
                      size='icon'
                      variant='ghost'
                      className='size-7 shrink-0'
                      aria-label={t('Move down')}
                      disabled={index === members.length - 1}
                      onClick={() => moveMember(index, 1)}
                    >
                      <ArrowDown className='size-3.5' />
                    </Button>
                    <Button
                      type='button'
                      size='icon'
                      variant='ghost'
                      className='size-7 shrink-0'
                      aria-label={t('Remove member')}
                      onClick={() => removeMember(index)}
                    >
                      <X className='size-3.5' />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>

      <div className='flex shrink-0 items-center justify-end gap-2'>
        <Button type='button' variant='outline' onClick={props.onCancel}>
          {t('Cancel')}
        </Button>
        <Button
          type='button'
          disabled={!canSave}
          title={saveDisabledReason}
          onClick={() => save.mutate()}
        >
          {save.isPending && <Loader2 className='size-4 animate-spin' />}
          {t('Save')}
        </Button>
      </div>
    </div>
  )
}
