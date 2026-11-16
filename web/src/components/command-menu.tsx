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
import { useLocation, useNavigate } from '@tanstack/react-router'
import {
  ArrowRight,
  ChevronRight,
  Laptop,
  Moon,
  Search,
  Sun,
} from 'lucide-react'
import React from 'react'
import { useTranslation } from 'react-i18next'

import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'
import { useSearch } from '@/context/search-context'
import { useTheme } from '@/context/theme-provider'
import { useDebounce } from '@/hooks/use-debounce'
import { useSidebarData } from '@/hooks/use-sidebar-data'

import { CommandEntityGroups } from './command-menu/entity-groups'
import {
  useCommandEntitySearch,
  type CommandEntity,
} from './command-menu/entity-search'
import { getNavGroupsForPath } from './layout/lib/sidebar-view-registry'
import { ScrollArea } from './ui/scroll-area'

export function CommandMenu() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { setTheme } = useTheme()
  const { open, setOpen } = useSearch()
  const { pathname } = useLocation()
  const sidebarData = useSidebarData()
  const [term, setTerm] = React.useState('')

  // ui-spec §12: 250ms debounce, at most five rows per entity group, only
  // existing read-only endpoints.
  const debouncedTerm = useDebounce(term, 250)
  const { groups, isLoading } = useCommandEntitySearch(debouncedTerm, open)

  // Use the active nested sidebar view's nav groups when one matches
  // the current URL; otherwise fall back to the root navigation.
  const navGroups = getNavGroupsForPath(pathname, t) ?? sidebarData.navGroups

  const runCommand = React.useCallback(
    (command: () => unknown) => {
      setOpen(false)
      setTerm('')
      command()
    },
    [setOpen]
  )

  const runEntity = React.useCallback(
    (entity: CommandEntity) => {
      runCommand(() => {
        const target = entity.target
        switch (target.kind) {
          case 'channel':
            void navigate({
              to: '/connections/channels',
              search: { filter: target.name },
            })
            return
          case 'key':
            void navigate({ to: '/keys', search: { filter: target.name } })
            return
          case 'model':
            void navigate({
              to: '/connections/catalog',
              search: { filter: target.model },
            })
            return
          case 'lane':
            void navigate({ to: '/routes', search: { model: target.model } })
            return
          case 'request': {
            // `/api/v1/logs` cannot filter by request id, so the deep link
            // narrows the request-log view by the fields taken from the
            // fetched row (lane/model/channel/key + exact second). The id is
            // carried in the URL for the filter bar.
            const search: Record<string, string | number> = {
              requestId: String(target.id),
            }
            if (target.lane !== '') search.lane = target.lane
            if (target.channel !== '') search.channel = target.channel
            if (target.key !== '') search.token = target.key
            if (target.model !== '') search.model = target.model
            if (target.ts > 0) {
              search.startTime = target.ts * 1000
              search.endTime = target.ts * 1000
            }
            void navigate({ to: '/logs/requests', search })
            return
          }
          default:
            return
        }
      })
    },
    [navigate, runCommand]
  )

  return (
    <CommandDialog
      modal
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen)
        if (!nextOpen) setTerm('')
      }}
      title={t('Command palette')}
      description={t(
        'Search commands, channels, keys, models, routes or request IDs'
      )}
    >
      <Command>
        <CommandInput
          value={term}
          onValueChange={setTerm}
          placeholder={t(
            'Search commands, channels, keys, models, routes or request IDs'
          )}
        />
        <CommandList>
          <ScrollArea className='h-72 pe-1'>
            {!isLoading && (
              <CommandEmpty>{t('No results found.')}</CommandEmpty>
            )}
            {isLoading && (
              <div
                role='status'
                className='text-muted-foreground text-caption flex items-center gap-2 px-3 py-2'
              >
                <Search className='size-3.5 animate-pulse' aria-hidden='true' />
                {t('Searching…')}
              </div>
            )}
            {groups.length > 0 && (
              <>
                <CommandEntityGroups groups={groups} onSelect={runEntity} />
                <CommandSeparator />
              </>
            )}
            {navGroups.map((group) => (
              <CommandGroup key={group.id || group.title} heading={group.title}>
                {group.items.map((navItem) => {
                  if (navItem.url) {
                    return (
                      <CommandItem
                        key={navItem.url}
                        value={navItem.title}
                        onSelect={() => {
                          runCommand(() => navigate({ to: navItem.url }))
                        }}
                      >
                        <div className='flex size-4 items-center justify-center'>
                          <ArrowRight className='text-muted-foreground/80 size-2' />
                        </div>
                        {navItem.title}
                      </CommandItem>
                    )
                  }

                  return navItem.items?.map((subItem) => (
                    <CommandItem
                      key={`${navItem.title}-${subItem.url}`}
                      value={`${navItem.title}-${subItem.url}`}
                      onSelect={() => {
                        runCommand(() => navigate({ to: subItem.url }))
                      }}
                    >
                      <div className='flex size-4 items-center justify-center'>
                        <ArrowRight className='text-muted-foreground/80 size-2' />
                      </div>
                      {navItem.title} <ChevronRight /> {subItem.title}
                    </CommandItem>
                  ))
                })}
              </CommandGroup>
            ))}
            <CommandSeparator />
            <CommandGroup heading={t('Actions')}>
              <CommandItem
                value='playground'
                onSelect={() => {
                  runCommand(() => navigate({ to: '/playground' }))
                }}
              >
                <ArrowRight className='text-muted-foreground/80 size-2' />
                <span>{t('Playground')}</span>
              </CommandItem>
            </CommandGroup>
            <CommandGroup heading={t('Theme')}>
              <CommandItem onSelect={() => runCommand(() => setTheme('light'))}>
                <Sun /> <span>{t('Light')}</span>
              </CommandItem>
              <CommandItem onSelect={() => runCommand(() => setTheme('dark'))}>
                <Moon className='scale-90' />
                <span>{t('Dark')}</span>
              </CommandItem>
              <CommandItem
                onSelect={() => runCommand(() => setTheme('system'))}
              >
                <Laptop />
                <span>{t('System')}</span>
              </CommandItem>
            </CommandGroup>
          </ScrollArea>
        </CommandList>
      </Command>
    </CommandDialog>
  )
}
