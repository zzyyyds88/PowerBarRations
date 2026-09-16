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
import { lazy, Suspense, useCallback } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { FadeIn } from '@/components/page-transition'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { OverviewDashboard } from './components/overview/overview-dashboard'
import {
  type DashboardSectionId,
  DASHBOARD_DEFAULT_SECTION,
  DASHBOARD_SECTION_IDS,
} from './section-registry'

const route = getRouteApi('/_authenticated/dashboard/$section')

const LazyModelAnalytics = lazy(() =>
  import('./components/cost/cost-dashboard').then((m) => ({
    default: m.ModelAnalytics,
  }))
)

const LazyCostDashboard = lazy(() =>
  import('./components/cost/cost-dashboard').then((m) => ({
    default: m.CostDashboard,
  }))
)

const SECTION_META: Record<DashboardSectionId, { titleKey: string }> = {
  overview: { titleKey: 'Overview' },
  models: { titleKey: 'Model Call Analytics' },
  cost: { titleKey: 'Cost analytics' },
}

function AnalyticsFallback() {
  return (
    <div className='space-y-4'>
      <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
        {['a', 'b', 'c', 'd'].map((key) => (
          <Skeleton key={key} className='h-[68px] rounded-lg' />
        ))}
      </div>
      <Skeleton className='h-56 rounded-lg' />
      <Skeleton className='h-64 rounded-lg' />
    </div>
  )
}

export function Dashboard() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const params = route.useParams()
  const activeSection = (params.section ??
    DASHBOARD_DEFAULT_SECTION) as DashboardSectionId
  const meta = SECTION_META[activeSection] ?? SECTION_META.overview
  const visibleSections = DASHBOARD_SECTION_IDS.filter(
    (section) => section !== 'overview'
  )
  const handleSectionChange = useCallback(
    (section: string) => {
      void navigate({
        to: '/dashboard/$section',
        params: { section: section as DashboardSectionId },
      })
    },
    [navigate]
  )

  if (activeSection === 'overview') {
    return <OverviewDashboard />
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t(meta.titleKey)}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-3 sm:space-y-4'>
          {visibleSections.length > 1 && (
            <Tabs value={activeSection} onValueChange={handleSectionChange}>
              <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
                {visibleSections.map((section) => (
                  <TabsTrigger key={section} value={section}>
                    {t(SECTION_META[section].titleKey)}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
          )}
          {activeSection === 'models' && (
            <FadeIn>
              <Suspense fallback={<AnalyticsFallback />}>
                <LazyModelAnalytics />
              </Suspense>
            </FadeIn>
          )}
          {activeSection === 'cost' && (
            <FadeIn>
              <Suspense fallback={<AnalyticsFallback />}>
                <LazyCostDashboard />
              </Suspense>
            </FadeIn>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
