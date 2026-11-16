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
import {
  ArrowDown,
  ArrowUp,
  GripVertical,
  Layers,
  Trash2,
  X,
} from 'lucide-react'
import type { DragEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import type {
  PBRLaneMode,
  PBRChannelCatalogEntry,
  PBRMemberGroup,
} from '../api'
import {
  isModelDeclaredByChannel,
  resolvedUpstreamForMember,
} from '../lib/lane-member-upstream'
import type { ComposerMember } from './lane-composer-types'

interface LaneComposerMemberListProps {
  members: ComposerMember[]
  memberGroups: PBRMemberGroup[]
  catalog: PBRChannelCatalogEntry[]
  duplicateNames: string[]
  mode: PBRLaneMode
  activeMember: string
  draggedId: string
  dragOverId: string
  dragOverPosition: 'before' | 'after'
  onDragStart: (event: DragEvent, memberId: string) => void
  onDragOver: (event: DragEvent, memberId: string) => void
  onDrop: (event: DragEvent, memberId: string) => void
  onDragEnd: () => void
  onToggleEnabled: (index: number, enabled: boolean) => void
  onMove: (index: number, delta: number) => void
  onRemove: (index: number) => void
  onClear: () => void
}

export function LaneComposerMemberList(props: LaneComposerMemberListProps) {
  const { t } = useTranslation()
  const emptyMembers = props.members.length === 0

  return (
    <div className='flex min-h-0 flex-col rounded-md border'>
      <div className='flex shrink-0 items-center justify-between border-b px-3 py-2'>
        <span className='text-sm font-medium'>
          {t('Members (order)')}
          {props.members.length > 0 && (
            <span className='text-muted-foreground ml-1 text-xs font-normal'>
              ({props.members.length})
            </span>
          )}
        </span>
        <Button
          type='button'
          size='sm'
          variant='ghost'
          disabled={emptyMembers}
          onClick={props.onClear}
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
            {props.members.map((member, index) => {
              // 组引用行（ADR 0009）：单行组元素（组名 + 成员数 + 展开预览 +
              // 本车道引用开关 + 上下移/删除）。组内成员的增删/顺序/停定在组
              // 管理里维护，这里只读展示（展开子行）。
              if (member.group) {
                const group = props.memberGroups.find(
                  (item) => item.name === member.group
                )
                return (
                  <div
                    key={member.id}
                    onDragOver={(event) => props.onDragOver(event, member.id)}
                    onDrop={(event) => props.onDrop(event, member.id)}
                    className={cn(
                      'rounded-md border p-2',
                      props.draggedId === member.id && 'opacity-50',
                      props.dragOverId === member.id &&
                        props.draggedId !== member.id &&
                        (props.dragOverPosition === 'before'
                          ? 'border-t-2 border-t-primary'
                          : 'border-b-2 border-b-primary'),
                      !member.enabled && 'bg-muted/40',
                      !group?.enabled && 'opacity-50'
                    )}
                  >
                    <div className='flex items-center gap-2'>
                      <span
                        role='button'
                        tabIndex={-1}
                        draggable
                        aria-label={t('Drag to reorder')}
                        title={t('Drag to reorder')}
                        onDragStart={(event) =>
                          props.onDragStart(event, member.id)
                        }
                        onDragEnd={props.onDragEnd}
                        className='text-muted-foreground hover:text-foreground shrink-0 cursor-grab active:cursor-grabbing'
                      >
                        <GripVertical className='size-4' />
                      </span>
                      <span className='text-muted-foreground w-5 shrink-0 text-center text-xs'>
                        {index + 1}
                      </span>
                      <div className='min-w-0 flex-1'>
                        <div className='flex items-center gap-1.5 truncate font-mono text-xs font-medium'>
                          <Layers className='text-muted-foreground size-3.5 shrink-0' />
                          {member.group}
                          <span className='text-muted-foreground/70 shrink-0'>
                            ({group?.members.length ?? '-'})
                          </span>
                        </div>
                        {!group?.enabled && (
                          <p className='text-muted-foreground mt-0.5 text-[11px]'>
                            {t('Group is disabled globally')}
                          </p>
                        )}
                      </div>
                      <div className='flex shrink-0 items-center gap-1'>
                        <Switch
                          size='sm'
                          checked={member.enabled}
                          aria-label={t(
                            'Group reference enabled for {{group}}',
                            { group: member.group }
                          )}
                          onCheckedChange={(checked) =>
                            props.onToggleEnabled(index, checked)
                          }
                        />
                        <Button
                          type='button'
                          size='icon'
                          variant='ghost'
                          className='size-8 shrink-0'
                          aria-label={t('Move up')}
                          disabled={index === 0}
                          onClick={() => props.onMove(index, -1)}
                        >
                          <ArrowUp className='size-3.5' />
                        </Button>
                        <Button
                          type='button'
                          size='icon'
                          variant='ghost'
                          className='size-8 shrink-0'
                          aria-label={t('Move down')}
                          disabled={index === props.members.length - 1}
                          onClick={() => props.onMove(index, 1)}
                        >
                          <ArrowDown className='size-3.5' />
                        </Button>
                        <Button
                          type='button'
                          size='icon'
                          variant='ghost'
                          className='size-8 shrink-0'
                          aria-label={t('Remove member')}
                          onClick={() => props.onRemove(index)}
                        >
                          <X className='size-3.5' />
                        </Button>
                      </div>
                    </div>
                    <ul className='border-border/60 mt-1.5 space-y-0.5 rounded-md border-l-2 pt-1 pl-3'>
                      {(group?.members ?? []).map((item) => (
                        <li
                          key={`${item.channel}\u0000${item.model}`}
                          className={cn(
                            'text-muted-foreground truncate font-mono text-[11px]',
                            item.enabled === false && 'opacity-50'
                          )}
                          title={`${item.channel} / ${item.model}`}
                        >
                          {item.channel} · {item.model}
                          {item.enabled === false && (
                            <span className='ml-1'>
                              ({t('Manually disabled')})
                            </span>
                          )}
                        </li>
                      ))}
                      {group && group.members.length === 0 && (
                        <li className='text-muted-foreground text-[11px]'>
                          {t('This group has no members.')}
                        </li>
                      )}
                    </ul>
                  </div>
                )
              }
              // 防呆告警（ui-spec §6.3）：成员所选模型不在该渠道声明范围内时
              // 行内提示，**不拦截保存**。判据是**所选模型**（渠道 models 或
              // model_mapping 左键），不再是解析后的真名。
              const undeclared = !isModelDeclaredByChannel(
                member.channel,
                member.model,
                props.catalog
              )
              const duplicated = props.duplicateNames.includes(member.channel)
              const disabled = !member.enabled
              // manual 的 active_member 指向被停用成员：运行期必然"无可用"且
              // 不换人（routing-spec §2.1）。界面**行内告警但不拦保存**——
              // "先关成员、再改别的配置"是合法中间态（api-spec §4.2）。
              const activeDisabled =
                props.mode === 'manual' &&
                disabled &&
                (props.activeMember === member.publicAlias ||
                  props.activeMember === `${member.channel}/${member.model}`)
              return (
                <div
                  key={member.id}
                  onDragOver={(event) => props.onDragOver(event, member.id)}
                  onDrop={(event) => props.onDrop(event, member.id)}
                  className={cn(
                    'rounded-md border p-2',
                    // 拖拽反馈：被拖起的行降透明度、落点行显示上/下指示线。
                    props.draggedId === member.id && 'opacity-50',
                    props.dragOverId === member.id &&
                      props.draggedId !== member.id &&
                      (props.dragOverPosition === 'before'
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
                        props.onDragStart(event, member.id)
                      }
                      onDragEnd={props.onDragEnd}
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
                          props.catalog
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
                          props.onToggleEnabled(index, checked)
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
                      className='size-8 shrink-0'
                      aria-label={t('Move up')}
                      disabled={index === 0}
                      onClick={() => props.onMove(index, -1)}
                    >
                      <ArrowUp className='size-3.5' />
                    </Button>
                    <Button
                      type='button'
                      size='icon'
                      variant='ghost'
                      className='size-8 shrink-0'
                      aria-label={t('Move down')}
                      disabled={index === props.members.length - 1}
                      onClick={() => props.onMove(index, 1)}
                    >
                      <ArrowDown className='size-3.5' />
                    </Button>
                    <Button
                      type='button'
                      size='icon'
                      variant='ghost'
                      className='size-8 shrink-0'
                      aria-label={t('Remove member')}
                      onClick={() => props.onRemove(index)}
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
                    <p className='text-warning mt-1.5 text-xs'>
                      {t(
                        'This member is the active member of a manual lane and is disabled, so the model returns no available member.'
                      )}
                    </p>
                  )}
                  {duplicated && (
                    <p className='text-warning mt-1.5 text-xs' role='alert'>
                      {t(
                        'This channel name is shared by multiple channels; lane members cannot be addressed unambiguously. Rename the duplicate first.'
                      )}
                    </p>
                  )}
                  {!duplicated && undeclared && (
                    <p className='text-warning mt-1.5 text-xs'>
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
  )
}
