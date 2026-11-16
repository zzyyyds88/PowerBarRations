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
import { Loader2 } from 'lucide-react'
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'

import {
  BUILTIN_LANE_DEFAULTS,
  listPBRChannelCatalog,
  listPBRMemberGroups,
  listPBRModels,
  loadLaneDefaults,
  memberKey,
  pbrModelsQueryKey,
  savePBRFailover,
  type PBRLaneMode,
} from '../api'
import { duplicateChannelNames } from '../lib/lane-member-upstream'
import { priorityForIndex, reorderMembers } from '../lib/reorder-members'
import { LaneComposerConfigPanel } from './lane-composer-config-panel'
import { LaneComposerMemberList } from './lane-composer-member-list'
import type { ComposerMember, LaneComposerProps } from './lane-composer-types'

export type { ComposerMember, LaneComposerProps } from './lane-composer-types'

// 单调递增的成员 id：稳定、无随机性，测试可复现。
let memberIdSeq = 0
function nextMemberId(): string {
  memberIdSeq += 1
  return `m${memberIdSeq}`
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
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const configTouched = useRef(false)
  const defaultsApplied = useRef(false)
  const laneDefaultsQuery = useQuery({
    queryKey: ['lane-defaults'],
    queryFn: loadLaneDefaults,
    enabled: !editing,
  })
  useEffect(() => {
    if (
      editing ||
      defaultsApplied.current ||
      configTouched.current ||
      !laneDefaultsQuery.data
    ) {
      return
    }
    defaultsApplied.current = true
    setConfig({ ...BUILTIN_LANE_DEFAULTS, ...laneDefaultsQuery.data })
  }, [editing, laneDefaultsQuery.data])
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
  // 新建模式必须知道路由键是否已存在：PUT /lanes 是全量 upsert，直接保存会
  // 静默覆盖既有车道的成员/模式/六键。
  const laneKeysQuery = useQuery({
    queryKey: pbrModelsQueryKey,
    queryFn: listPBRModels,
    enabled: !editing,
  })
  const existingLane = Boolean(
    !editing &&
    laneKeysQuery.data?.some(
      (item) => item.model === name.trim() && item.source !== 'unconfigured'
    )
  )
  const laneKeysReady = editing || laneKeysQuery.isSuccess
  // 成员组目录（ADR 0009）：左栏「成员组」小节与右栏组引用行展开预览共用。
  const groupsQuery = useQuery({
    queryKey: ['pbr-member-groups'],
    queryFn: listPBRMemberGroups,
  })
  const memberGroups = useMemo(() => groupsQuery.data ?? [], [groupsQuery.data])
  // 展开的组引用行（一次展开一个足够；组内成员只读预览）。
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
  // **组不排他（ADR 0009 修订）**：组内成员与散成员允许重叠——判重只在散成员
  // 集合内做，组在链上时左栏仍可选组内的 (渠道, 模型) 作为散成员加入。
  const selectedKeys = useMemo(
    () =>
      new Set(
        members
          .filter((member) => !member.group)
          .map((member) =>
            memberKey({ channel: member.channel, model: member.model })
          )
      ),
    [members]
  )

  const addGroupRef = (groupName: string) => {
    // 同组允许重复引用多次（组不排他）：每次引用 = 链上一个独立区段。
    markDirty([
      ...members,
      {
        id: nextMemberId(),
        channel: '',
        model: '',
        group: groupName,
        enabled: true,
      },
    ])
  }

  const addMember = (channel: string, model: string) => {
    // 成员只存"用户点的这个模型"；上游真名不进草稿落库字段，只在展示时派生
    // （ADR 0008）。去重键 = (渠道, 所选模型)：同一渠道的不同模型仍可多次加入。
    const key = memberKey({ channel, model })
    if (members.some((member) => memberKey(member) === key)) {
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
    configTouched.current = true
    setConfig((prev) => ({ ...prev, [key]: value }))
  }

  const save = useMutation({
    mutationFn: async () => {
      await savePBRFailover(
        routeKey,
        members.map((member, index) => {
          const priority = priorityForIndex(index, members.length)
          // 组引用元素（ADR 0009 两态）：只带 group/enabled/priority——组内成员
          // 在组实体上维护，链上只表达"这个位置放这个组"。
          if (member.group) {
            return { group: member.group, priority, enabled: member.enabled }
          }
          return {
            // 写端点成员字段：model 必填；**不发 upstream_model**（服务端派生只读，
            // 写端点根本不接受该字段，ADR 0008）。
            channel: member.channel,
            model: member.model,
            // 读写闭环（api-spec §4.2 通则）：成员写入是全量替换，草稿里读到的每个
            // 成员级字段都必须原样带回，否则"打开编辑再保存"会把它们清空。
            public_alias: member.publicAlias ?? '',
            priority,
            enabled: member.enabled,
            overrides: member.overrides ?? {},
          }
        }),
        {
          mode,
          activeMember: mode === 'manual' ? activeMember : '',
          config,
        }
      )
    },
    onSuccess: async () => {
      let successMessage = editing ? 'Failover order saved' : 'Lane saved'
      if (props.initiallyDisabled) {
        successMessage = 'Lane saved and re-enabled'
      }
      toast.success(t(successMessage))
      await queryClient.invalidateQueries({ queryKey: ['pbr-routable-models'] })
      await queryClient.invalidateQueries({ queryKey: ['pbr-route'] })
      // 组引用的 used-by 列表会随车道保存变化。
      await queryClient.invalidateQueries({ queryKey: ['pbr-member-groups'] })
      await props.onSaved()
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : String(error))
    },
  })

  const trimmedKey = routeKey
  const emptyMembers = members.length === 0
  const duplicateNames = useMemo(
    () => duplicateChannelNames(catalog),
    [catalog]
  )
  const hasDuplicateSelected = members.some((member) =>
    duplicateNames.includes(member.channel)
  )
  // manual 点名候选：后端同时接受 public_alias 与 channel/model，界面校验
  // 也必须接受两种写法；否则已有 alias 的成员存着 label 时会被误判为失效。
  const activeMemberCandidates = new Set<string>()
  let unresolvedGroup = false
  for (const member of members) {
    if (member.group) {
      const group = memberGroups.find((item) => item.name === member.group)
      if (!group) {
        unresolvedGroup = true
        continue
      }
      for (const item of group.members) {
        activeMemberCandidates.add(`${item.channel}/${item.model}`)
        if (item.public_alias) activeMemberCandidates.add(item.public_alias)
      }
      continue
    }
    activeMemberCandidates.add(`${member.channel}/${member.model}`)
    if (member.publicAlias) activeMemberCandidates.add(member.publicAlias)
  }
  const manualActiveInvalid =
    mode === 'manual' &&
    activeMember !== '' &&
    !unresolvedGroup &&
    !activeMemberCandidates.has(activeMember)
  const manualMissingActive =
    mode === 'manual' && (activeMember === '' || manualActiveInvalid)
  // 点名成员可能仍然解析得到，但它/所在组被人工停用：请求同样会 503，
  // 必须在此提示，而不是等运行期报错。
  let activeMemberDisabled = false
  if (mode === 'manual' && activeMember !== '') {
    for (const member of members) {
      if (member.group) {
        const group = memberGroups.find((item) => item.name === member.group)
        if (!group) continue
        const groupOff = member.enabled === false || !group.enabled
        for (const item of group.members) {
          const matches =
            activeMember === `${item.channel}/${item.model}` ||
            (item.public_alias !== undefined &&
              item.public_alias === activeMember)
          if (matches && (groupOff || item.enabled === false)) {
            activeMemberDisabled = true
          }
        }
        continue
      }
      const matches =
        activeMember === `${member.channel}/${member.model}` ||
        (member.publicAlias !== undefined &&
          member.publicAlias === activeMember)
      if (matches && member.enabled === false) activeMemberDisabled = true
    }
  }
  const canSave =
    trimmedKey.length > 0 &&
    !emptyMembers &&
    !manualMissingActive &&
    !existingLane &&
    laneKeysReady &&
    !hasDuplicateSelected &&
    !save.isPending

  let saveDisabledReason: string | undefined
  if (trimmedKey.length === 0) {
    saveDisabledReason = t('Enter a route key first.')
  } else if (emptyMembers) {
    saveDisabledReason = t('Keep at least one member, or remove the lane.')
  } else if (existingLane) {
    saveDisabledReason = t(
      'A lane with this route key already exists. Open it from the list to edit instead.'
    )
  } else if (!laneKeysReady) {
    saveDisabledReason = t(
      'Could not verify existing route keys. Retry before creating a lane.'
    )
  } else if (manualMissingActive) {
    saveDisabledReason = t(
      'Pick the active member for manual mode before saving.'
    )
  } else if (hasDuplicateSelected) {
    saveDisabledReason = t('Resolve duplicate channel names before saving.')
  }

  // manual 点名候选：链上散成员 + 组引用展开的组内成员（ADR 0009：manual 点名
  // 组内成员的标签照旧 `channel/model` 或组内成员别名）。组引用行本身没有
  // channel/model，不作为候选。
  const memberOptions = members.flatMap((member) => {
    if (member.group) {
      const group = memberGroups.find((item) => item.name === member.group)
      return (group?.members ?? []).map((item) => ({
        value: item.public_alias || `${item.channel}/${item.model}`,
        label: item.public_alias
          ? `${item.public_alias} (${item.channel})`
          : `${item.channel} / ${item.model} · ${member.group}`,
      }))
    }
    const alias = member.publicAlias ?? ''
    // active_member 的标签取 `channel/model`（model = 成员所选模型，ADR 0008）：
    // 成员身份稳定，不随渠道映射改动漂移（api-spec §4.2）。
    const value = alias || `${member.channel}/${member.model}`
    return [
      {
        value,
        label: alias
          ? `${alias} (${member.channel})`
          : `${member.channel} / ${member.model}`,
      },
    ]
  })
  // 触发框显示所选成员的标签；找不到匹配项时回退原始值（不静默显示空）。
  const activeMemberLabel =
    memberOptions.find((option) => option.value === activeMember)?.label ??
    activeMember

  return (
    <div className='flex min-h-0 flex-1 flex-col gap-3'>
      {duplicateNames.length > 0 && (
        <p className='text-warning text-xs' role='alert'>
          {t(
            'Duplicate channel names: {{names}}. Rename the duplicates in Channel Management before saving lane members.',
            { names: duplicateNames.join(', ') }
          )}
        </p>
      )}
      {/* 左右两栏（ui-spec §6.3，2026-10-25 重排）：左栏 = 配置（路由键/模式/
          manual 点名/高级六键）+ 添加成员选择器（含「成员组」小节）；右栏整栏
          只放「成员（顺序）」。 */}
      <div className='grid min-h-0 flex-1 grid-cols-1 gap-3 md:grid-cols-[2fr_3fr]'>
        <LaneComposerConfigPanel
          routeKey={routeKey}
          editing={editing}
          existingLane={existingLane}
          mode={mode}
          activeMember={activeMember}
          activeMemberLabel={activeMemberLabel}
          memberOptions={memberOptions}
          manualActiveInvalid={manualActiveInvalid}
          activeMemberDisabled={activeMemberDisabled}
          advancedOpen={advancedOpen}
          config={config}
          memberGroups={memberGroups}
          catalogLoading={catalogQuery.isLoading}
          catalog={catalog}
          selectedKeys={selectedKeys}
          onNameChange={(value) => {
            setName(value)
            props.onDirtyChange?.(true)
          }}
          onModeChange={(value) => {
            setMode(value)
            if (value !== 'manual') setActiveMember('')
            props.onDirtyChange?.(true)
          }}
          onActiveMemberChange={(value) => {
            setActiveMember(value)
            props.onDirtyChange?.(true)
          }}
          onAdvancedOpenChange={setAdvancedOpen}
          onUpdateConfig={(key, value) => {
            updateConfig(key, value)
            props.onDirtyChange?.(true)
          }}
          onAddMember={addMember}
          onAddGroupRef={addGroupRef}
        />
        <LaneComposerMemberList
          members={members}
          memberGroups={memberGroups}
          catalog={catalog}
          duplicateNames={duplicateNames}
          mode={mode}
          activeMember={activeMember}
          draggedId={draggedId}
          dragOverId={dragOverId}
          dragOverPosition={dragOverPosition}
          onDragStart={handleDragStart}
          onDragOver={handleDragOver}
          onDrop={handleDrop}
          onDragEnd={resetDragState}
          onToggleEnabled={toggleMemberEnabled}
          onMove={moveMember}
          onRemove={removeMember}
          onClear={() => markDirty([])}
        />
      </div>

      {props.initiallyDisabled && (
        <p className='text-warning shrink-0 text-xs' role='alert'>
          {t(
            'This lane is currently disabled. Saving will re-enable it and traffic will resume.'
          )}
        </p>
      )}
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
