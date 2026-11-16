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
import { Layers, Loader2, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'

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
import { cn } from '@/lib/utils'

import {
  BUILTIN_LANE_DEFAULTS,
  memberKey,
  type PBRLaneMode,
  type PBRChannelCatalogEntry,
  type PBRMemberGroup,
} from '../api'
import { MemberPickerAccordion } from './member-picker'

interface ActiveMemberOption {
  value: string
  label: string
}

interface LaneComposerConfigPanelProps {
  routeKey: string
  editing: boolean
  existingLane: boolean
  mode: PBRLaneMode
  activeMember: string
  activeMemberLabel: string
  memberOptions: ActiveMemberOption[]
  manualActiveInvalid: boolean
  activeMemberDisabled: boolean
  advancedOpen: boolean
  config: Record<string, number>
  memberGroups: PBRMemberGroup[]
  catalogLoading: boolean
  catalog: PBRChannelCatalogEntry[]
  selectedKeys: Set<string>
  onNameChange: (value: string) => void
  onModeChange: (value: PBRLaneMode) => void
  onActiveMemberChange: (value: string) => void
  onAdvancedOpenChange: (open: boolean) => void
  onUpdateConfig: (key: string, value: number) => void
  onAddMember: (channel: string, model: string) => void
  onAddGroupRef: (groupName: string) => void
}

export function LaneComposerConfigPanel(props: LaneComposerConfigPanelProps) {
  const { t } = useTranslation()

  let leftPanelContent
  if (props.catalogLoading) {
    leftPanelContent = (
      <p className='text-muted-foreground flex items-center gap-2 p-2 text-xs'>
        <Loader2 className='size-3.5 animate-spin' /> {t('Loading...')}
      </p>
    )
  } else {
    leftPanelContent = (
      <MemberPickerAccordion
        catalog={props.catalog}
        selectedKeys={props.selectedKeys}
        keyOf={(channel, model) => memberKey({ channel, model })}
        onAdd={props.onAddMember}
      />
    )
  }

  return (
    <div className='flex min-h-0 flex-col gap-3'>
      <div className='space-y-1.5'>
        <Label htmlFor='lane-route-key'>{t('Route key')}</Label>
        <Input
          id='lane-route-key'
          value={props.routeKey}
          readOnly={props.editing}
          disabled={props.editing}
          placeholder={t('e.g. my-pooled-model')}
          onChange={(event) => {
            props.onNameChange(event.target.value)
          }}
        />
        {props.existingLane && (
          <p className='text-warning text-xs' role='alert'>
            {t(
              'A lane with this route key already exists. Open it from the list to edit; saving here would overwrite its members.'
            )}
          </p>
        )}
      </div>
      <div className='space-y-1.5'>
        <Label>{t('Mode')}</Label>
        <Select
          value={props.mode}
          onValueChange={(value) => {
            props.onModeChange(value === 'manual' ? 'manual' : 'failover')
          }}
        >
          <SelectTrigger aria-label={t('Mode')} className='w-full'>
            {/* 显式给 SelectValue 子节点：Base UI 默认回显原始值（failover），
                会绕过 i18n，中文界面显示英文。 */}
            <SelectValue>{t(props.mode)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='failover'>{t('failover')}</SelectItem>
            <SelectItem value='manual'>{t('manual')}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {props.mode === 'manual' && (
        <div className='space-y-1.5'>
          <Label>{t('Active member')}</Label>
          <Select
            value={props.activeMember}
            onValueChange={(value) => {
              props.onActiveMemberChange(value ?? '')
            }}
          >
            <SelectTrigger aria-label={t('Active member')} className='w-full'>
              <SelectValue placeholder={t('Select a member')}>
                {props.activeMemberLabel}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {props.memberOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {props.manualActiveInvalid && (
            <p className='text-warning text-xs' role='alert'>
              {t(
                'The selected active member is no longer in the chain. Pick another one.'
              )}
            </p>
          )}
          {props.activeMemberDisabled && (
            <p className='text-warning text-xs' role='alert'>
              {t(
                'The selected active member is disabled or its group is off; requests will return no available member.'
              )}
            </p>
          )}
        </div>
      )}

      <Collapsible
        open={props.advancedOpen}
        onOpenChange={props.onAdvancedOpenChange}
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
                value={String(props.config[key] ?? BUILTIN_LANE_DEFAULTS[key])}
                onChange={(event) => {
                  const parsed = Number.parseInt(event.target.value, 10)
                  props.onUpdateConfig(
                    key,
                    Number.isFinite(parsed) && parsed >= 0 ? parsed : 0
                  )
                }}
              />
            </div>
          ))}
        </CollapsibleContent>
      </Collapsible>

      {/* 左栏选料卡：成员组小节 + 渠道 → 模型选择器（占左栏剩余高度）。 */}
      <div className='flex min-h-0 flex-1 flex-col rounded-md border'>
        <div className='flex shrink-0 items-center gap-2 border-b px-3 py-2'>
          <span className='text-sm font-medium'>{t('Add members')}</span>
        </div>
        <div className='min-h-0 flex-1 overflow-y-auto p-2'>
          <div className='mb-2'>
            <p className='text-muted-foreground mb-1 px-1 text-[11px] font-medium uppercase'>
              {t('Member groups')}
            </p>
            {props.memberGroups.length === 0 ? (
              <p className='text-muted-foreground px-1 text-xs'>
                {t(
                  'No member groups yet. Create one from the routes page header to reuse a member chain across lanes.'
                )}
              </p>
            ) : (
              <div className='space-y-1'>
                {props.memberGroups.map((group) => {
                  return (
                    <button
                      key={group.name}
                      type='button'
                      onClick={() => props.onAddGroupRef(group.name)}
                      title={group.name}
                      className={cn(
                        'flex w-full items-center gap-2 rounded-md border px-2 py-1.5 text-left text-xs transition-colors',
                        'hover:bg-muted'
                      )}
                    >
                      <Layers className='text-muted-foreground size-3.5 shrink-0' />
                      <span className='min-w-0 flex-1 truncate font-mono'>
                        {group.name}
                      </span>
                      {!group.enabled && (
                        <span className='text-muted-foreground shrink-0 text-[10px]'>
                          {t('off')}
                        </span>
                      )}
                      <span className='text-muted-foreground shrink-0'>
                        {group.members.length}
                      </span>
                      <Plus className='size-3.5 shrink-0' />
                    </button>
                  )
                })}
              </div>
            )}
          </div>
          {leftPanelContent}
        </div>
      </div>
    </div>
  )
}
