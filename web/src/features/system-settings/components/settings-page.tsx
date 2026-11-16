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
import { Link, useLocation, useParams } from '@tanstack/react-router'
import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { ConsolePage } from '@/components/layout'
import { Skeleton } from '@/components/ui/skeleton'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { getContentSectionNavItems } from '../content/section-registry.tsx'
import { useSystemOptions, getOptionValue } from '../hooks/use-system-options'
import { getModelsSectionNavItems } from '../models/section-registry.tsx'
import { getOperationsSectionNavItems } from '../operations/section-registry.tsx'
import { getSiteSectionNavItems } from '../site/section-registry.tsx'
import type { SystemOption } from '../types'
import { SettingsPageProvider } from './settings-page-context'

type SettingsPageProps<
  TSettings extends Record<string, string | number | boolean | unknown[]>,
  TSectionId extends string,
  TExtraArgs extends unknown[] = [],
> = {
  routePath: string
  defaultSettings: TSettings
  defaultSection: TSectionId
  getSectionContent: (
    sectionId: TSectionId,
    settings: TSettings,
    ...extraArgs: TExtraArgs
  ) => ReactNode
  getSectionMeta: (sectionId: TSectionId) => {
    titleKey: string
  }
  extraArgs?: TExtraArgs
  loadingMessage?: string
  resolveSettings?: (
    settings: TSettings,
    raw: SystemOption[] | undefined
  ) => TSettings
}

type SettingsPageFrameProps = {
  title: ReactNode
  children: ReactNode
}

function SettingsSectionTabs() {
  const { t } = useTranslation()
  const pathname = useLocation({ select: (location) => location.pathname })
  const items = useMemo(() => {
    if (pathname.startsWith('/system-settings/site')) {
      return getSiteSectionNavItems(t)
    }
    if (pathname.startsWith('/system-settings/models')) {
      return getModelsSectionNavItems(t)
    }
    if (pathname.startsWith('/system-settings/content')) {
      return getContentSectionNavItems(t)
    }
    if (pathname.startsWith('/system-settings/operations')) {
      return getOperationsSectionNavItems(t)
    }
    return []
  }, [pathname, t])

  if (items.length < 2) return null
  return (
    <nav className='border-border flex max-w-full items-center gap-4 overflow-x-auto border-b'>
      {items.map((item) => {
        const active = pathname === item.url
        return (
          <Link
            key={item.url}
            to={item.url}
            aria-current={active ? 'page' : undefined}
            className={
              active
                ? 'border-foreground text-foreground -mb-px shrink-0 border-b-2 px-0.5 pb-2 text-sm font-medium'
                : 'text-muted-foreground hover:text-foreground -mb-px shrink-0 border-b-2 border-transparent px-0.5 pb-2 text-sm transition-colors'
            }
          >
            {item.title}
          </Link>
        )
      })}
    </nav>
  )
}

function SettingsPageFrame(props: SettingsPageFrameProps) {
  const [actionsContainer, setActionsContainer] =
    useState<HTMLDivElement | null>(null)
  const [titleStatusContainer, setTitleStatusContainer] =
    useState<HTMLSpanElement | null>(null)

  return (
    <SettingsPageProvider
      actionsContainer={actionsContainer}
      titleStatusContainer={titleStatusContainer}
    >
      <ConsolePage
        title={
          <span className='inline-flex max-w-full min-w-0 items-center gap-2 align-middle'>
            <span className='truncate'>{props.title}</span>
            <span
              ref={setTitleStatusContainer}
              className='inline-flex min-w-0 shrink-0 items-center'
            />
          </span>
        }
        actions={
          <div
            ref={setActionsContainer}
            className='flex flex-wrap items-center justify-end gap-2'
          />
        }
        stackActionsOnMobile={false}
        tabs={<SettingsSectionTabs />}
      >
        <div className='flex h-full min-h-0 w-full flex-col gap-4'>
          {props.children}
        </div>
      </ConsolePage>
    </SettingsPageProvider>
  )
}

function SettingsPageSkeleton(props: { label: string }) {
  return (
    <div
      role='status'
      aria-busy='true'
      aria-label={props.label}
      className='space-y-4'
    >
      <Skeleton className='h-8 w-52' />
      <div className='border-border/60 bg-card space-y-3 rounded-lg border p-4'>
        <Skeleton className='h-4 w-32' />
        <Skeleton className='h-9 w-full' />
        <Skeleton className='h-9 w-full' />
      </div>
      <div className='border-border/60 bg-card space-y-3 rounded-lg border p-4'>
        <Skeleton className='h-4 w-40' />
        <Skeleton className='h-9 w-full' />
        <Skeleton className='h-9 w-2/3' />
      </div>
    </div>
  )
}

/**
 * Generic settings page component
 * Handles loading state, data fetching, and section rendering
 */
export function SettingsPage<
  TSettings extends Record<string, string | number | boolean | unknown[]>,
  TSectionId extends string,
  TExtraArgs extends unknown[] = [],
>({
  routePath,
  defaultSettings,
  defaultSection,
  getSectionContent,
  getSectionMeta,
  extraArgs,
  loadingMessage = 'Loading settings...',
  resolveSettings,
}: SettingsPageProps<TSettings, TSectionId, TExtraArgs>) {
  const { t } = useTranslation()
  const { data, isLoading, isError, error, refetch } = useSystemOptions()
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const params = useParams({ from: routePath as any })
  const activeSection = (params?.section ?? defaultSection) as TSectionId
  const sectionMeta = getSectionMeta(activeSection)

  const settings = useMemo(() => {
    const baseSettings = getOptionValue(data, defaultSettings) as TSettings
    return resolveSettings ? resolveSettings(baseSettings, data) : baseSettings
  }, [data, defaultSettings, resolveSettings])

  if (isLoading) {
    return (
      <SettingsPageFrame title={t(sectionMeta.titleKey)}>
        <SettingsPageSkeleton label={t(loadingMessage)} />
      </SettingsPageFrame>
    )
  }

  // Settings must never fall back to their defaults (or render an empty form)
  // because the options request failed.
  if (isError) {
    return (
      <SettingsPageFrame title={t(sectionMeta.titleKey)}>
        <ErrorState
          title={t('Failed to load settings')}
          description={getServerErrorMessage(error)}
          onRetry={() => void refetch()}
          className='min-h-40'
        />
      </SettingsPageFrame>
    )
  }

  const sectionContent = getSectionContent(
    activeSection,
    settings,
    ...((extraArgs ?? []) as TExtraArgs)
  )

  return (
    <SettingsPageFrame title={t(sectionMeta.titleKey)}>
      {sectionContent}
    </SettingsPageFrame>
  )
}
