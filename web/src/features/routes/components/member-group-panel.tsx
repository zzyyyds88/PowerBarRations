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
成员组左栏面板（ui-spec §6.3，2026-10-25 重排）：路由与故障切换页拆两栏后，
左栏（约 1/4 宽）常驻列出全部成员组——组名 + 全局启停 + 展开的组内成员清单，
独立上下滚动；「新建组」与点组名打开编辑弹窗（MemberGroupEditorDialog），
删除走 destructive 确认并展示引用车道。
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ChevronDown,
  Layers,
  Loader2,
  Pencil,
  Plus,
  Trash2,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  deletePBRMemberGroup,
  listPBRMemberGroups,
  updatePBRMemberGroup,
  type PBRMemberGroup,
} from '../api'
import { MemberGroupEditorDialog } from './member-group-manager-dialog'

export function MemberGroupPanel(props: {
  collapsible?: boolean
  focusGroup?: string
  onFocusGroupClose?: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  // undefined = 关闭；'' = 新建；其余 = 编辑该组名。
  const [editingName, setEditingName] = useState<string | undefined>(undefined)
  const [editorOpen, setEditorOpen] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<PBRMemberGroup | null>(
    null
  )

  const groupsQuery = useQuery({
    queryKey: ['pbr-member-groups'],
    queryFn: listPBRMemberGroups,
  })
  const groups = useMemo(() => groupsQuery.data ?? [], [groupsQuery.data])

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['pbr-member-groups'] })
    await queryClient.invalidateQueries({ queryKey: ['pbr-routable-models'] })
  }

  const toggleEnabled = useMutation({
    mutationFn: (input: { name: string; enabled: boolean }) =>
      updatePBRMemberGroup(input.name, {
        name: input.name,
        enabled: input.enabled,
      }),
    onSuccess: (_, input) => {
      toast.success(
        input.enabled
          ? t('Group {{group}} enabled', { group: input.name })
          : t(
              'Group {{group}} disabled: it is now skipped on every lane using it',
              {
                group: input.name,
              }
            )
      )
      return invalidate()
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : String(error))
    },
  })

  const remove = useMutation({
    mutationFn: (name: string) => deletePBRMemberGroup(name),
    onSuccess: async (result) => {
      const details =
        result.manual_lanes_cleared.length > 0
          ? t('Manual lanes now need an active member: {{lanes}}.', {
              lanes: result.manual_lanes_cleared.join(', '),
            })
          : undefined
      toast.success(
        t(
          'Group removed. {{cleaned}} lane(s) updated; {{deleted}} empty lane(s) deleted.',
          {
            cleaned: result.cleaned_lanes.length,
            deleted: result.deleted_lanes.length,
          }
        ),
        details ? { description: details } : undefined
      )
      setPendingDelete(null)
      await invalidate()
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : String(error))
    },
  })

  const openEditor = (groupName: string) => {
    setEditingName(groupName)
    setEditorOpen(true)
  }

  // 冲突对话框的 `/routes?group=...` 深链：组数据就绪后打开该组编辑器。
  const focusedGroup = useRef('')
  useEffect(() => {
    const target = props.focusGroup?.trim()
    if (!target || focusedGroup.current === target) return
    if (!groups.some((group) => group.name === target)) return
    focusedGroup.current = target
    // 微任务里打开编辑器：避免在 effect 同步阶段触发级联渲染。
    queueMicrotask(() => openEditor(target))
  }, [groups, props.focusGroup])

  let listContent
  if (groupsQuery.isLoading) {
    listContent = (
      <div className='text-muted-foreground flex items-center gap-2 p-2 text-xs'>
        <Loader2 className='size-3.5 animate-spin' /> {t('Loading...')}
      </div>
    )
  } else if (groupsQuery.isError) {
    listContent = (
      <div className='flex flex-col items-start gap-2 p-2'>
        <p className='text-destructive text-xs' role='alert'>
          {t('Failed to load member groups, please retry.')}
        </p>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => void groupsQuery.refetch()}
        >
          {t('Retry')}
        </Button>
      </div>
    )
  } else if (groups.length === 0) {
    listContent = (
      <p className='text-muted-foreground p-2 text-xs'>
        {t(
          'No member groups yet. Create one to reuse a member chain across lanes.'
        )}
      </p>
    )
  } else {
    listContent = (
      <ul className='space-y-1.5'>
        {groups.map((group) => (
          <li
            key={group.name}
            className={cn(
              'rounded-md border p-2',
              !group.enabled && 'bg-muted/40'
            )}
          >
            <div className='flex items-center gap-1.5'>
              <Switch
                size='sm'
                checked={group.enabled}
                aria-label={t('Group enabled for {{group}}', {
                  group: group.name,
                })}
                onCheckedChange={(checked) =>
                  toggleEnabled.mutate({ name: group.name, enabled: checked })
                }
              />
              <button
                type='button'
                className='min-w-0 flex-1 truncate text-left font-mono text-xs font-medium'
                title={group.name}
                onClick={() => openEditor(group.name)}
              >
                {group.name}
              </button>
              <Button
                type='button'
                size='icon'
                variant='ghost'
                className='size-8 shrink-0'
                aria-label={t('Edit member group')}
                onClick={() => openEditor(group.name)}
              >
                <Pencil className='size-3' />
              </Button>
              <Button
                type='button'
                size='icon'
                variant='ghost'
                className='text-destructive hover:text-destructive size-8 shrink-0'
                aria-label={t('Delete group')}
                onClick={() => setPendingDelete(group)}
              >
                <Trash2 className='size-3' />
              </Button>
            </div>
            {/* 组内成员默认展开（左栏即成员清单，不再折叠）。 */}
            <ul className='border-border/60 mt-1 space-y-0.5 border-l-2 pl-2.5'>
              {group.members.map((item) => (
                <li
                  key={`${item.channel}\u0000${item.model}`}
                  className={cn(
                    'text-muted-foreground truncate font-mono text-[11px]',
                    item.enabled === false && 'opacity-50'
                  )}
                  title={`${item.channel} · ${item.model}`}
                >
                  {item.channel} · {item.model}
                  {item.enabled === false && (
                    <span className='ml-1'>({t('off')})</span>
                  )}
                </li>
              ))}
              {group.members.length === 0 && (
                <li className='text-muted-foreground text-[11px]'>
                  {t('This group has no members.')}
                </li>
              )}
            </ul>
            {(group.referenced_by?.length ?? 0) > 0 && (
              <p className='text-muted-foreground/70 mt-1 truncate text-[10px]'>
                {t('used by')} {group.referenced_by?.join(', ')}
              </p>
            )}
          </li>
        ))}
      </ul>
    )
  }

  const collapsible = props.collapsible ?? false
  const showList = !collapsible || open

  return (
    <div
      aria-label={t('Member groups')}
      className={cn(
        'border-border rounded-md border',
        collapsible ? 'shrink-0' : 'flex h-full min-h-0 flex-col'
      )}
    >
      <div className='flex shrink-0 items-center gap-1.5 border-b px-2.5 py-2'>
        <Layers className='text-muted-foreground size-3.5' />
        {collapsible ? (
          <button
            type='button'
            className='flex min-w-0 flex-1 items-center gap-1.5 text-left'
            aria-expanded={open}
            onClick={() => setOpen((value) => !value)}
          >
            <span className='text-sm font-medium'>{t('Member groups')}</span>
            <span className='text-muted-foreground text-xs tabular-nums'>
              {groups.length}
            </span>
            <ChevronDown
              className={cn(
                'text-muted-foreground ms-auto size-4 transition-transform',
                open && 'rotate-180'
              )}
            />
          </button>
        ) : (
          <span className='min-w-0 flex-1 truncate text-sm font-medium'>
            {t('Member groups')}
          </span>
        )}
        <Button
          type='button'
          size='sm'
          variant='ghost'
          className='h-7 shrink-0 px-2'
          onClick={() => openEditor('')}
        >
          <Plus className='size-3.5' />
          {t('New group')}
        </Button>
      </div>
      {showList && (
        <div
          className={cn(
            'p-2',
            collapsible
              ? 'max-h-[45vh] overflow-y-auto'
              : 'min-h-0 flex-1 overflow-y-auto'
          )}
        >
          {listContent}
        </div>
      )}

      {editorOpen && (
        <MemberGroupEditorDialog
          open
          onOpenChange={(open) => {
            setEditorOpen(open)
            if (!open) {
              focusedGroup.current = ''
              props.onFocusGroupClose?.()
            }
          }}
          groupName={editingName ?? ''}
          onSaved={invalidate}
        />
      )}

      <ConfirmDialog
        destructive
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
        title={t('Delete this member group?')}
        desc={t(
          'The group reference is removed from every lane using it; lanes left with no members are deleted. Referenced by: {{lanes}}',
          { lanes: pendingDelete?.referenced_by?.join(', ') || t('none') }
        )}
        confirmText={t('Delete group')}
        isLoading={remove.isPending}
        handleConfirm={() => {
          if (pendingDelete) remove.mutate(pendingDelete.name)
        }}
      />
    </div>
  )
}
