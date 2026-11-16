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
成员组管理弹窗（ADR 0009、ui-spec §6.3「成员组管理入口」）：路由页页头
「成员组」按钮打开。列表 + 创建/编辑/删除；编辑 = 组名、组全局启停开关、
组内成员有序列表（添加/上下移/启停/别名/六键覆盖/删除——与车道散成员同构，
复用渠道目录选择器）。删除走 destructive 确认并展示 referenced_by。
*/
import { useMutation, useQuery } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, Layers, Loader2, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Dialog as DialogRoot,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  createPBRMemberGroup,
  listPBRChannelCatalog,
  listPBRMemberGroups,
  memberKey,
  updatePBRMemberGroup,
  type PBRMemberGroup,
} from '../api'
import { MemberPickerAccordion } from './member-picker'

/** 组内成员草稿（与散成员同构）。 */
interface GroupMemberDraft {
  id: string
  channel: string
  model: string
  /** 读写闭环：组保存是全量替换，漏掉 alias 会在每次保存时静默清空。 */
  publicAlias?: string
  enabled: boolean
  overrides: Record<string, number>
}

let draftSeq = 0
function nextDraftId(): string {
  draftSeq += 1
  return `gd${draftSeq}`
}

/**
 * 成员组编辑弹窗（路由页左栏面板的编辑入口，ui-spec §6.3）：
 * `groupName` 空串 = 新建；非空 = 编辑该组。列表展示由左栏面板承担。
 */
export function MemberGroupEditorDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 空串 = 新建；非空 = 编辑该组名。 */
  groupName: string
  onSaved: () => Promise<void> | void
}) {
  const { t } = useTranslation()
  const groupsQuery = useQuery({
    queryKey: ['pbr-member-groups'],
    queryFn: listPBRMemberGroups,
    enabled: props.open,
  })
  const editingGroup = props.groupName
    ? (groupsQuery.data ?? []).find((g) => g.name === props.groupName)
    : undefined
  const [dirty, setDirty] = useState(false)
  const [discardOpen, setDiscardOpen] = useState(false)

  const close = () => {
    setDirty(false)
    props.onOpenChange(false)
  }
  const requestClose = () => {
    if (dirty) {
      setDiscardOpen(true)
      return
    }
    close()
  }

  // 关闭时清空草稿状态；用渲染期调整代替 setState-in-effect。
  const [prevOpen, setPrevOpen] = useState(props.open)
  if (props.open !== prevOpen) {
    setPrevOpen(props.open)
    if (!props.open) {
      setDirty(false)
      setDiscardOpen(false)
    }
  }

  let editorBody
  if (!props.groupName) {
    editorBody = (
      <GroupEditor
        key='new'
        groupName=''
        onDone={close}
        onCancel={requestClose}
        onDirtyChange={setDirty}
        onSaved={props.onSaved}
      />
    )
  } else if (groupsQuery.isLoading) {
    editorBody = (
      <div className='text-muted-foreground flex items-center gap-2 p-2 text-sm'>
        <Loader2 className='size-4 animate-spin' /> {t('Loading...')}
      </div>
    )
  } else if (groupsQuery.isError && !editingGroup) {
    editorBody = (
      <ErrorState
        description={t('Failed to load this member group, please retry.')}
        onRetry={() => void groupsQuery.refetch()}
        className='min-h-[240px]'
      />
    )
  } else if (!editingGroup) {
    editorBody = (
      <ErrorState
        description={t(
          'This member group no longer exists. Close the editor and refresh the list.'
        )}
        onRetry={() => void groupsQuery.refetch()}
        className='min-h-[240px]'
      />
    )
  } else {
    editorBody = (
      <GroupEditor
        key={props.groupName}
        groupName={props.groupName}
        existing={editingGroup}
        onDone={close}
        onCancel={requestClose}
        onDirtyChange={setDirty}
        onSaved={props.onSaved}
      />
    )
  }

  return (
    <DialogRoot
      open={props.open}
      onOpenChange={(open) => {
        if (open) props.onOpenChange(true)
        else requestClose()
      }}
    >
      <DialogContent
        variant='sheet'
        className='flex w-full flex-col gap-4 overflow-hidden p-4 sm:w-[min(98vw,1040px)] sm:max-w-none sm:p-6'
      >
        <DialogHeader className='pr-12'>
          <DialogTitle className='flex items-center gap-2'>
            <Layers className='size-4' />
            {props.groupName ? t('Edit member group') : t('New group')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'A member group is a reusable ordered member chain. Reference it from any lane; turning the group off disables it on every lane that uses it.'
            )}
          </DialogDescription>
        </DialogHeader>
        {/* 不要再包一层 overflow-y-auto：那会让 GroupEditor 失去高度约束，
            底部保存按钮随内容一起滚出弹窗。滚动交给左右两栏内部，与车道编辑器同构。 */}
        <div className='flex min-h-0 flex-1 flex-col'>{editorBody}</div>
      </DialogContent>
      <ConfirmDialog
        destructive
        open={discardOpen}
        onOpenChange={setDiscardOpen}
        title={t('Discard unsaved changes?')}
        desc={t(
          'The member group has unsaved changes. Closing now will discard them.'
        )}
        confirmText={t('Discard changes')}
        handleConfirm={() => {
          setDiscardOpen(false)
          close()
        }}
      />
    </DialogRoot>
  )
}

/** 创建/编辑一组（组名 + 全局开关 + 成员有序列表 + 添加成员选择器）。 */
function GroupEditor(props: {
  groupName: string
  existing?: PBRMemberGroup
  onDone: () => void
  onCancel: () => void
  onDirtyChange?: (dirty: boolean) => void
  onSaved: () => Promise<void> | void
}) {
  const { t } = useTranslation()
  const creating = props.groupName === ''
  const [name, setName] = useState(props.groupName)
  const [enabled, setEnabled] = useState(props.existing?.enabled ?? true)
  const [members, setMembers] = useState<GroupMemberDraft[]>(
    (props.existing?.members ?? []).map((m) => ({
      id: nextDraftId(),
      channel: m.channel,
      model: m.model,
      publicAlias: m.public_alias,
      enabled: m.enabled !== false,
      overrides: { ...m.overrides },
    }))
  )

  const snapshot = (value: {
    name: string
    enabled: boolean
    members: GroupMemberDraft[]
  }) =>
    JSON.stringify({
      name: value.name,
      enabled: value.enabled,
      members: value.members.map((m) => ({
        channel: m.channel,
        model: m.model,
        alias: m.publicAlias ?? '',
        enabled: m.enabled,
        overrides: m.overrides,
      })),
    })
  // Existing group is captured once when the editor mounts.
  const [initialSnapshot] = useState(() =>
    snapshot({
      name: props.groupName,
      enabled: props.existing?.enabled ?? true,
      members: (props.existing?.members ?? []).map((m) => ({
        id: '',
        channel: m.channel,
        model: m.model,
        publicAlias: m.public_alias,
        enabled: m.enabled !== false,
        overrides: { ...m.overrides },
      })),
    })
  )
  const dirty = snapshot({ name, enabled, members }) !== initialSnapshot
  const onDirtyChange = props.onDirtyChange
  useEffect(() => {
    onDirtyChange?.(dirty)
  }, [dirty, onDirtyChange])

  const catalogQuery = useQuery({
    queryKey: ['pbr-channel-catalog'],
    queryFn: listPBRChannelCatalog,
  })
  const catalog = useMemo(() => catalogQuery.data ?? [], [catalogQuery.data])

  const selectedKeys = useMemo(
    () => new Set(members.map((m) => memberKey(m))),
    [members]
  )

  const save = useMutation({
    mutationFn: async () => {
      const payload = {
        name: name.trim(),
        enabled,
        members: members.map((m, index) => ({
          channel: m.channel,
          model: m.model,
          public_alias: m.publicAlias ?? '',
          priority: members.length - index,
          enabled: m.enabled,
          overrides: m.overrides,
        })),
      }
      if (creating) {
        return createPBRMemberGroup(payload)
      }
      return updatePBRMemberGroup(props.groupName, payload)
    },
    onSuccess: async (saved) => {
      const manualLanes = saved.manual_lanes_cleared ?? []
      toast.success(
        t('Group saved'),
        manualLanes.length > 0
          ? {
              description: t(
                'Manual lanes now need an active member: {{lanes}}.',
                { lanes: manualLanes.join(', ') }
              ),
            }
          : undefined
      )
      await props.onSaved()
      props.onDone()
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : String(error))
    },
  })

  const move = (index: number, delta: number) => {
    const target = index + delta
    if (target < 0 || target >= members.length) return
    const next = [...members]
    const [item] = next.splice(index, 1)
    next.splice(target, 0, item)
    setMembers(next)
  }

  return (
    <div className='flex min-h-0 flex-1 flex-col gap-3'>
      <div className='flex shrink-0 items-end gap-2'>
        <div className='min-w-0 flex-1 space-y-1.5'>
          <Label>{t('Group name')}</Label>
          <Input
            value={name}
            readOnly={!creating}
            disabled={!creating}
            placeholder={t('e.g. dual-upstream')}
            onChange={(event) => setName(event.target.value)}
          />
        </div>
        <div className='flex shrink-0 items-center gap-2 pb-1.5'>
          <Switch
            size='sm'
            checked={enabled}
            aria-label={t('Group enabled for {{group}}', { group: name })}
            onCheckedChange={setEnabled}
          />
          <span className='text-muted-foreground text-xs'>{t('Enabled')}</span>
        </div>
      </div>

      {/* 左右两栏（与车道编排器同款，ui-spec §6.3）：左 = 渠道折叠选料树，
          右 = 组内成员有序列表。 */}
      <div className='grid min-h-0 flex-1 grid-cols-1 gap-3 md:grid-cols-2'>
        <div className='flex min-h-0 flex-col rounded-md border'>
          <div className='flex shrink-0 items-center gap-2 border-b px-3 py-2'>
            <span className='text-sm font-medium'>{t('Add members')}</span>
          </div>
          <div className='min-h-0 flex-1 overflow-y-auto p-2'>
            <MemberPickerAccordion
              catalog={catalog}
              loading={catalogQuery.isLoading}
              selectedKeys={selectedKeys}
              keyOf={(channel, model) => memberKey({ channel, model })}
              onAdd={(channel, model) =>
                setMembers((prev) => [
                  ...prev,
                  {
                    id: nextDraftId(),
                    channel,
                    model,
                    enabled: true,
                    overrides: {},
                  },
                ])
              }
            />
          </div>
        </div>

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
            {members.length > 0 && (
              <Button
                type='button'
                size='sm'
                variant='ghost'
                className='h-7 px-2'
                onClick={() => setMembers([])}
              >
                <X className='size-3.5' />
                {t('Clear')}
              </Button>
            )}
          </div>
          <div className='min-h-0 flex-1 overflow-y-auto p-2'>
            {members.length === 0 ? (
              <p className='text-muted-foreground p-2 text-xs'>
                {t('Pick models above to add group members.')}
              </p>
            ) : (
              <div className='space-y-1.5'>
                {members.map((member, index) => (
                  <div
                    key={member.id}
                    className={cn(
                      'flex items-center gap-2 rounded-md border p-2',
                      !member.enabled && 'bg-muted/40'
                    )}
                  >
                    <span className='text-muted-foreground w-5 shrink-0 text-center text-xs'>
                      {index + 1}
                    </span>
                    <div className='min-w-0 flex-1 truncate font-mono text-xs'>
                      <span className='font-medium'>{member.channel}</span>
                      <span className='text-muted-foreground'>
                        {' '}
                        · {member.model}
                      </span>
                    </div>
                    <div className='flex shrink-0 items-center gap-1'>
                      <Switch
                        size='sm'
                        checked={member.enabled}
                        aria-label={t('Member enabled for {{channel}}', {
                          channel: `${member.channel} · ${member.model}`,
                        })}
                        onCheckedChange={(checked) =>
                          setMembers((prev) =>
                            prev.map((m) =>
                              m.id === member.id
                                ? { ...m, enabled: checked }
                                : m
                            )
                          )
                        }
                      />
                      <Button
                        type='button'
                        size='icon'
                        variant='ghost'
                        className='size-8'
                        aria-label={t('Move up')}
                        disabled={index === 0}
                        onClick={() => move(index, -1)}
                      >
                        <ArrowUp className='size-3.5' />
                      </Button>
                      <Button
                        type='button'
                        size='icon'
                        variant='ghost'
                        className='size-8'
                        aria-label={t('Move down')}
                        disabled={index === members.length - 1}
                        onClick={() => move(index, 1)}
                      >
                        <ArrowDown className='size-3.5' />
                      </Button>
                      <Button
                        type='button'
                        size='icon'
                        variant='ghost'
                        className='size-8'
                        aria-label={t('Remove member')}
                        onClick={() =>
                          setMembers((prev) =>
                            prev.filter((m) => m.id !== member.id)
                          )
                        }
                      >
                        <X className='size-3.5' />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>

      <div className='flex shrink-0 items-center justify-end gap-2'>
        <Button size='sm' variant='outline' onClick={props.onCancel}>
          {t('Cancel')}
        </Button>
        <Button
          size='sm'
          disabled={!name.trim() || members.length === 0 || save.isPending}
          onClick={() => save.mutate()}
        >
          {save.isPending && <Loader2 className='size-4 animate-spin' />}
          {t('Save')}
        </Button>
      </div>
    </div>
  )
}
