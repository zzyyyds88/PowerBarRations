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
  Boxes,
  Cpu,
  FileSearch,
  KeyRound,
  Waypoints,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CommandGroup, CommandItem } from '@/components/ui/command'
import { cn } from '@/lib/utils'

import type {
  CommandEntity,
  CommandEntityGroup,
  CommandEntityKind,
} from './entity-search'

const GROUP_LABEL_KEY: Record<CommandEntityKind, string> = {
  channel: 'Channels',
  key: 'API Keys',
  model: 'Models',
  lane: 'Routes',
  request: 'Request ID',
}

const GROUP_ICON: Record<CommandEntityKind, LucideIcon> = {
  channel: Boxes,
  key: KeyRound,
  model: Cpu,
  lane: Waypoints,
  request: FileSearch,
}

function entityHint(entity: CommandEntity, t: (key: string) => string) {
  const parts = [...(entity.hintParts ?? [])]
  if (entity.disabled === true) parts.push(t('Disabled'))
  if (entity.kind === 'request') {
    parts.push(entity.success ? t('Success') : t('Failed'))
  }
  return parts.join(' · ')
}

/**
 * Entity results for the command palette (ui-spec §12).
 *
 * One `CommandGroup` per entity kind, at most five rows each (the hook
 * already slices). Rows are ordinary `CommandItem`s, so arrow keys, Enter and
 * the visible focus state come from cmdk; `value` keeps cmdk's own filter
 * consistent with the pre-filtered rows.
 */
export function CommandEntityGroups(props: {
  groups: CommandEntityGroup[]
  onSelect: (entity: CommandEntity) => void
}) {
  const { t } = useTranslation()

  return (
    <>
      {props.groups.map((group) => {
        const Icon = GROUP_ICON[group.id]
        return (
          <CommandGroup key={group.id} heading={t(GROUP_LABEL_KEY[group.id])}>
            {group.items.map((entity) => {
              const hint = entityHint(entity, t)
              return (
                <CommandItem
                  key={entity.id}
                  value={`${entity.label} ${entity.id} ${entity.hintParts?.join(' ') ?? ''}`}
                  onSelect={() => props.onSelect(entity)}
                >
                  <Icon
                    className='text-muted-foreground/80 size-4'
                    aria-hidden='true'
                  />
                  <span className='min-w-0 truncate'>{entity.label}</span>
                  {hint !== '' && (
                    <span
                      className={cn(
                        'text-muted-foreground ml-auto max-w-[50%] truncate text-caption'
                      )}
                    >
                      {hint}
                    </span>
                  )}
                </CommandItem>
              )
            })}
          </CommandGroup>
        )
      })}
    </>
  )
}
