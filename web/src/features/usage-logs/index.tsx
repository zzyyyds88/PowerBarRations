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
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import type { NavGroup } from '@/components/layout/types'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useSidebarConfig } from '@/hooks/use-sidebar-config'

import { UserInfoDialog } from './components/dialogs/user-info-dialog'
import {
  type LogsViewScope,
  UsageLogsProvider,
  useLogsViewScope,
  useUsageLogsContext,
} from './components/usage-logs-provider'
import { UsageLogsTable } from './components/usage-logs-table'
import { PBRLaneLogsSection } from './pbr/components/pbr-lane-logs-section'
import {
  isUsageLogsSectionId,
  USAGE_LOGS_DEFAULT_SECTION,
  type UsageLogsSectionId,
} from './section-registry'

const route = getRouteApi('/_authenticated/usage-logs/$section')
const REQUEST_LOG_SECTIONS = ['pbr'] as const

const SECTION_META: Record<UsageLogsSectionId, { titleKey: string }> = {
  pbr: {
    titleKey: 'Usage Logs',
  },
}

function UsageLogsContent() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const params = route.useParams()
  const activeCategory: UsageLogsSectionId =
    params.section && isUsageLogsSectionId(params.section)
      ? params.section
      : USAGE_LOGS_DEFAULT_SECTION
  const { selectedUserId, userInfoDialogOpen, setUserInfoDialogOpen } =
    useUsageLogsContext()
  const { canManageScope, viewScope, setViewScope } = useLogsViewScope()
  const tabNavGroups = useMemo<NavGroup[]>(
    () => [
      {
        title: 'Request Logs',
        items: REQUEST_LOG_SECTIONS.map((section) => ({
          title: SECTION_META[section].titleKey,
          url: `/usage-logs/${section}`,
        })),
      },
    ],
    []
  )
  const filteredTabGroups = useSidebarConfig(tabNavGroups)
  const visibleSections = useMemo(
    () =>
      filteredTabGroups
        .flatMap((group) => group.items ?? [])
        .map((item) => {
          if (!('url' in item) || typeof item.url !== 'string') return null
          return item.url.split('/').pop() ?? null
        })
        .filter((section): section is UsageLogsSectionId =>
          Boolean(section && isUsageLogsSectionId(section))
        ),
    [filteredTabGroups]
  )

  const handleSectionChange = useCallback(
    (section: string) => {
      void navigate({
        to: '/usage-logs/$section',
        params: { section: section as UsageLogsSectionId },
      })
    },
    [navigate]
  )

  const handleViewScopeChange = useCallback(
    (scope: string) => {
      if (scope === 'all' || scope === 'self') {
        setViewScope(scope as LogsViewScope)
      }
    },
    [setViewScope]
  )

  const pageMeta = SECTION_META[activeCategory]
  const showSectionSwitcher = visibleSections.length > 1
  const showScopeTabs = canManageScope && activeCategory !== 'pbr'

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>
          {t(pageMeta.titleKey)}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          {showScopeTabs && (
            <Tabs value={viewScope} onValueChange={handleViewScopeChange}>
              <TabsList>
                <TabsTrigger value='all'>{t('All')}</TabsTrigger>
                <TabsTrigger value='self'>{t('Only Mine')}</TabsTrigger>
              </TabsList>
            </Tabs>
          )}
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex h-full min-h-0 flex-col gap-4'>
            {showSectionSwitcher && (
              <Tabs value={activeCategory} onValueChange={handleSectionChange}>
                <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
                  {visibleSections.map((section) => (
                    <TabsTrigger key={section} value={section}>
                      {t(SECTION_META[section].titleKey)}
                    </TabsTrigger>
                  ))}
                </TabsList>
              </Tabs>
            )}
            <div className='min-h-0 flex-1'>
              {activeCategory === 'pbr' ? (
                <PBRLaneLogsSection />
              ) : (
                <UsageLogsTable logCategory={activeCategory} />
              )}
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <UserInfoDialog
        userId={selectedUserId}
        open={userInfoDialogOpen}
        onOpenChange={setUserInfoDialogOpen}
      />
    </>
  )
}

export function UsageLogs() {
  return (
    <UsageLogsProvider>
      <UsageLogsContent />
    </UsageLogsProvider>
  )
}
