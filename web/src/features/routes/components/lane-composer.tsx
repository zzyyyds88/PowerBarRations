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
车道成员编排器（ui-spec §6.3、ADR 0008）：新建车道与编辑成员链共用。

两栏布局（对齐既有分组编辑器的交互范式）：
- 左栏「添加成员」：从 GET /api/v1/channels 拉全部启用渠道的 models，按渠道折叠，
  带搜索；点某个模型即把「该渠道 × 该模型」加入右栏。**没有「自动添加」**。
- 右栏「成员（顺序）」：已选成员有序列表，上移/下移、拖拽、删除、启停开关、清空。

成员唯一键 = **(渠道, 所选模型)**：同一渠道的不同模型可在一条车道内出现多次，
同一模型不可重复（左栏对已加入的 (渠道, 模型) 直接禁用）。成员**只存所选模型**，
上游真名一律由 `渠道 model_mapping[所选模型] ?? 所选模型` 派生——成员行只读展示
解析结果，**不再提供成员级改名输入框**。路由键可任意命名，无需任何渠道声明过它；
候选为空时仍可保存（只要已选成员非空）。
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowUp,
  GripVertical,
  Loader2,
  Plus,
  Search,
  Trash2,
  X,
} from 'lucide-react'
import { useCallback, useMemo, useState, type DragEvent } from 'react'
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
import { Switch } from '@/components/ui/switch'
import { getLobeIcon } from '@/lib/lobe-icon'
import { resolveModelProvider } from '@/lib/model-provider'
import { cn } from '@/lib/utils'

import {
  BUILTIN_LANE_DEFAULTS,
  listPBRChannelCatalog,
  memberKey,
  savePBRFailover,
  type PBRLaneMode,
  type PBRMemberOverrides,
} from '../api'
import {
  isModelDeclaredByChannel,
  resolvedUpstreamForMember,
} from '../lib/lane-member-upstream'
import { priorityForIndex, reorderMembers } from '../lib/reorder-members'

/** 编排器里的一个已选成员（草稿态）。 */
export interface ComposerMember {
  /**
   * 稳定标识：仅用于 React key / 拖拽身份。不能把 channel 或 model 拼进 key
   * ——同一渠道可含多个模型，拼进去会让行身份随选择变化漂移。
   * 也**不得**用后端的 `member_id`：成员写入是整体替换，保存一次全部重算
   * （api-spec §5.7）。
   */
  id: string
  channel: string
  /**
   * 成员**所选**模型（成员身份与唯一键的一半，保存写回用它）。
   * 必须来自渠道声明的模型清单——它是成员表唯一落库的模型字段。
   */
  model: string
  /** 成员别名（可选，仅用于 manual 的 active_member 取值）。 */
  publicAlias?: string
  /**
   * 成员级六键覆盖草稿。**必须原样回传**：成员写入是全量替换，读不到即保存清空
   * （api-spec §4.2 读写闭环）。
   */
  overrides?: PBRMemberOverrides
  /** 人工停用开关草稿；`false` = 不参与选路（仍留在链里，可逆）。 */
  enabled: boolean
}

export interface LaneComposerProps {
  /** 新建时为空；编辑既有车道时为该车道名（路由键只读）。 */
  model?: string
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

  const [name, setName] = useState(props.model ?? '')
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
  // 原生 HTML5 DnD 的拖拽态：拖起者、当前悬停行、落点在该行的上半还是下半。
  // 参照 param-override-editor-dialog.tsx 的 handleDragStart/Over/Drop + resetDragState。
  const [draggedId, setDraggedId] = useState('')
  const [dragOverId, setDragOverId] = useState('')
  const [dragOverPosition, setDragOverPosition] = useState<'before' | 'after'>(
    'before'
  )

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

  const resetDragState = useCallback(() => {
    setDraggedId('')
    setDragOverId('')
    setDragOverPosition('before')
  }, [])

  const handleDragStart = useCallback((event: DragEvent, memberId: string) => {
    setDraggedId(memberId)
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', memberId)
  }, [])

  const handleDragOver = useCallback(
    (event: DragEvent, memberId: string) => {
      event.preventDefault()
      if (!draggedId || draggedId === memberId) return
      // 落点按行高一半判定：上半插入到该行之前、下半之后。这条规则让"拖到末位"
      // 只需落在最后一行下半，不必依赖列表尾部空白区。
      const rect = event.currentTarget.getBoundingClientRect()
      const position: 'before' | 'after' =
        event.clientY - rect.top > rect.height / 2 ? 'after' : 'before'
      setDragOverId(memberId)
      setDragOverPosition(position)
      event.dataTransfer.dropEffect = 'move'
    },
    [draggedId]
  )

  const handleDrop = useCallback(
    (event: DragEvent, memberId: string) => {
      event.preventDefault()
      const sourceId = draggedId || event.dataTransfer.getData('text/plain')
      const position = dragOverId === memberId ? dragOverPosition : 'before'
      if (sourceId && memberId && sourceId !== memberId) {
        // 先算出结果再更新 state：把 onDirtyChange 放进 setState 更新器里会让
        // 副作用随更新器重放（StrictMode 下更新器会被调用两次）。
        const next = reorderMembers(members, sourceId, memberId, position)
        // reorderMembers 在无效输入时返回原引用：引用未变即没有真实重排，
        // 不置脏、不触发多余的保存提示。
        if (next !== members) {
          setMembers(next)
          props.onDirtyChange?.(true)
        }
      }
      resetDragState()
    },
    [draggedId, dragOverId, dragOverPosition, members, props, resetDragState]
  )

  const toggleMemberEnabled = (index: number, enabled: boolean) => {
    const next = [...members]
    next[index] = { ...next[index], enabled }
    markDirty(next)
  }

  // 左栏禁用判定与 addMember 去重共用同一键：`(渠道, 所选模型)`（ADR 0008）。
  // 用所选模型而非派生真名——真名会随渠道映射改动漂移，拿它去重会让
  // "同一模型换了映射"被误判成另一个成员。
  const selectedKeys = useMemo(
    () =>
      new Set(
        members.map((m) => memberKey({ channel: m.channel, model: m.model }))
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

  const addMember = (channel: string, model: string) => {
    // 成员只存"用户点的这个模型"；上游真名不进草稿落库字段，只在展示时派生
    // （ADR 0008）。去重键 = (渠道, 所选模型)：同一渠道的不同模型仍可多次加入。
    const key = memberKey({ channel, model })
    if (members.some((m) => memberKey(m) === key)) {
      return
    }
    markDirty([
      ...members,
      {
        id: nextMemberId(),
        channel,
        model,
        // 新增成员一律按启用起草：后端三态对"新建成员省略 enabled"也是启用，
        // 两边口径一致，避免用户以为加进来就是关的。
        enabled: true,
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

  const updateConfig = (key: string, value: number) => {
    setConfig((prev) => ({ ...prev, [key]: value }))
  }

  const save = useMutation({
    mutationFn: async () => {
      await savePBRFailover(
        routeKey,
        members.map((m, index) => ({
          channel: m.channel,
          // 写端点成员字段：model 必填；**不发 upstream_model**（服务端派生只读，
          // 写端点根本不接受该字段，ADR 0008）。
          model: m.model,
          // 读写闭环（api-spec §4.2 通则）：成员写入是全量替换，草稿里读到的每个
          // 成员级字段都必须原样带回，否则"打开编辑再保存"会把它们清空。
          public_alias: m.publicAlias ?? '',
          priority: priorityForIndex(index, members.length),
          enabled: m.enabled,
          overrides: m.overrides ?? {},
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
                    memberKey({ channel: channel.name, model })
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
    // active_member 的标签取 `channel/model`（model = 成员所选模型，ADR 0008）：
    // 成员身份稳定，不随渠道映射改动漂移（api-spec §4.2）。
    const value = alias || `${m.channel}/${m.model}`
    return {
      value,
      label: alias ? `${alias} (${m.channel})` : `${m.channel} / ${m.model}`,
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
                {members.map((member, index) => {
                  // 防呆告警（ui-spec §6.3）：成员所选模型不在该渠道声明范围内时
                  // 行内提示，**不拦截保存**。判据是**所选模型**（渠道 models 或
                  // model_mapping 左键），不再是解析后的真名。
                  const undeclared = !isModelDeclaredByChannel(
                    member.channel,
                    member.model,
                    catalog
                  )
                  const disabled = !member.enabled
                  // manual 的 active_member 指向被停用成员：运行期必然"无可用"且
                  // 不换人（routing-spec §2.1）。界面**行内告警但不拦保存**——
                  // "先关成员、再改别的配置"是合法中间态（api-spec §4.2）。
                  const activeDisabled =
                    mode === 'manual' &&
                    disabled &&
                    (activeMember === member.publicAlias ||
                      activeMember === `${member.channel}/${member.model}`)
                  return (
                    <div
                      key={member.id}
                      onDragOver={(event) => handleDragOver(event, member.id)}
                      onDrop={(event) => handleDrop(event, member.id)}
                      className={cn(
                        'rounded-md border p-2',
                        // 拖拽反馈：被拖起的行降透明度、落点行显示上/下指示线。
                        draggedId === member.id && 'opacity-50',
                        dragOverId === member.id &&
                          draggedId !== member.id &&
                          (dragOverPosition === 'before'
                            ? 'border-t-2 border-t-primary'
                            : 'border-b-2 border-b-primary'),
                        disabled && 'bg-muted/40'
                      )}
                    >
                      <div className='flex items-center gap-2'>
                        {/* 拖拽手柄：原生 HTML5 draggable，无第三方 dnd 依赖。
                            整行也可作为落点（onDragOver/onDrop 在外层），但只有手柄
                            可拖起，避免与行内输入框的文本选择冲突。 */}
                        <span
                          role='button'
                          tabIndex={-1}
                          draggable
                          aria-label={t('Drag to reorder')}
                          title={t('Drag to reorder')}
                          onDragStart={(event) =>
                            handleDragStart(event, member.id)
                          }
                          onDragEnd={resetDragState}
                          className='text-muted-foreground hover:text-foreground shrink-0 cursor-grab active:cursor-grabbing'
                        >
                          <GripVertical className='size-4' />
                        </span>
                        <span className='text-muted-foreground w-5 shrink-0 text-center text-xs'>
                          {index + 1}
                        </span>
                        <div className='min-w-0 flex-1'>
                          <div className='truncate font-mono text-xs font-medium'>
                            {member.channel}
                          </div>
                          <div className='text-muted-foreground truncate font-mono text-xs'>
                            {member.model}
                          </div>
                          {/* 上游真名只读派生（ADR 0008）：成员不落库真名，改渠道
                              model_mapping 即对所有成员生效。这里**渲染期**按当前渠道
                              目录派生（不是加入时算一次），所以编辑器开着时改映射也能
                              立即看到新真名。 */}
                          <div className='text-muted-foreground truncate text-xs'>
                            {t('Resolved upstream')}:{' '}
                            {resolvedUpstreamForMember(
                              member.channel,
                              member.model,
                              catalog
                            )}
                          </div>
                        </div>
                        {/* 成员启停开关：复用既有 Switch（web/AGENTS 强制检索复用）。
                            可访问名带渠道 + 所选模型：同一渠道可在一条车道里出现多次
                            （ADR 0008），只用渠道名会让多行开关重名、无法定位。
                            分隔符用 " · " 而非 "/"：i18next 会把插值里的斜杠转义成
                            `&#x2F;`，可访问名随之变形（实测：测试与读屏都取不到原值）。 */}
                        <div className='flex shrink-0 items-center gap-1'>
                          <Switch
                            size='sm'
                            checked={member.enabled}
                            aria-label={t('Member enabled for {{channel}}', {
                              channel: `${member.channel} · ${member.model}`,
                            })}
                            onCheckedChange={(checked) =>
                              toggleMemberEnabled(index, checked)
                            }
                          />
                          {disabled && (
                            <span className='text-muted-foreground text-xs'>
                              {t('Manually disabled')}
                            </span>
                          )}
                        </div>
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
                      {disabled && (
                        <p className='text-muted-foreground mt-1.5 text-xs'>
                          {t(
                            'This member is manually disabled: it stays in the chain but is skipped during routing. Re-enable it to bring it back.'
                          )}
                        </p>
                      )}
                      {activeDisabled && (
                        <p className='mt-1.5 text-xs text-amber-600 dark:text-amber-500'>
                          {t(
                            'This member is the active member of a manual lane and is disabled, so the model returns no available member.'
                          )}
                        </p>
                      )}
                      {undeclared && (
                        <p className='mt-1.5 text-xs text-amber-600 dark:text-amber-500'>
                          {t(
                            'Channel {{channel}} does not declare {{model}}; confirm the upstream supports this model name.',
                            { channel: member.channel, model: member.model }
                          )}
                        </p>
                      )}
                    </div>
                  )
                })}
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
