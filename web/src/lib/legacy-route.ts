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
import { CONSOLE_ROUTES, CONSOLE_ROUTE_PATHS } from '@/config/routes'

const legacyOrigin = 'https://legacy-route.invalid'

// Exact old-console paths -> 5+1 IA. Checked before prefix rules.
const legacyExactRoutes: Record<string, string> = {
  '/dashboard': CONSOLE_ROUTES.overview,
  '/dashboard/overview': CONSOLE_ROUTES.overview,
  '/dashboard/models': CONSOLE_ROUTES.overview,
  '/dashboard/cost': CONSOLE_ROUTES.overview,
  '/dashboard/flow': CONSOLE_ROUTES.overview,
  '/dashboard/users': CONSOLE_ROUTES.overview,
  '/channels': CONSOLE_ROUTES.connectionsChannels,
  '/channel-status': CONSOLE_ROUTES.connectionsHealth,
  '/models': CONSOLE_ROUTES.connectionsCatalog,
  '/models/metadata': CONSOLE_ROUTES.connectionsCatalog,
  '/models/routing': CONSOLE_ROUTES.connectionsCatalog,
  '/models/vendors': CONSOLE_ROUTES.connectionsCatalog,
  '/models/deployments': CONSOLE_ROUTES.connectionsCatalog,
  '/models/deployment': CONSOLE_ROUTES.connectionsCatalog,
  '/usage-logs': CONSOLE_ROUTES.logsRequests,
  '/usage-logs/pbr': CONSOLE_ROUTES.logsRequests,
  '/usage-logs/common': CONSOLE_ROUTES.logsRequests,
  '/usage-logs/audit': CONSOLE_ROUTES.logsAudit,
  '/usage-logs/task': CONSOLE_ROUTES.logsTasks,
  '/system-tasks': CONSOLE_ROUTES.logsTasks,
  '/console': CONSOLE_ROUTES.overview,
  '/console/models': CONSOLE_ROUTES.connectionsCatalog,
  '/console/channel': CONSOLE_ROUTES.connectionsChannels,
  '/console/token': CONSOLE_ROUTES.keys,
  '/console/playground': CONSOLE_ROUTES.playground,
  '/console/log': CONSOLE_ROUTES.logsRequests,
  '/console/task': CONSOLE_ROUTES.logsTasks,
  '/console/chat': CONSOLE_ROUTES.overview,
}

// Old settings tabs -> existing settings sections. Only fields that still exist.
const legacySettingsTabs: Record<string, string> = {
  operation: '/system-settings/operations/behavior',
  chats: '/system-settings/content/chat',
  models: '/system-settings/models/global',
  performance: '/system-settings/operations/performance',
  system: '/system-settings/site/system-info',
  other: '/system-settings/site/system-info',
}

function normalizeLegacyPath(pathname: string): string {
  if (pathname === '/') return pathname
  return pathname.replace(/\/+$/, '')
}

function buildTargetHref(targetPath: string, source: URL): string {
  const target = new URL(targetPath, legacyOrigin)
  source.searchParams.forEach((value, key) => {
    target.searchParams.append(key, value)
  })
  target.hash = source.hash
  return `${target.pathname}${target.search}${target.hash}`
}

export function resolveLegacyRoute(rawHref: string): string | null {
  let source: URL
  try {
    source = new URL(rawHref, legacyOrigin)
  } catch {
    return null
  }

  const pathname = normalizeLegacyPath(source.pathname)
  // Never touch API/static requests or the new console paths themselves.
  if (
    pathname.startsWith('/api/') ||
    pathname.startsWith('/static/') ||
    pathname.startsWith('/assets/') ||
    CONSOLE_ROUTE_PATHS.has(pathname)
  ) {
    return null
  }

  if (pathname === '/login') {
    return buildTargetHref('/sign-in', source)
  }
  if (pathname === '/forbidden') {
    return buildTargetHref('/403', source)
  }
  if (pathname === '/console/setting') {
    const tab = source.searchParams.get('tab') ?? ''
    const target = legacySettingsTabs[tab] ?? '/system-settings'
    return buildTargetHref(target, source)
  }
  if (pathname.startsWith('/console/chat/')) {
    const chatID = pathname.slice('/console/chat/'.length)
    return buildTargetHref(
      chatID ? `/chat/${chatID}` : CONSOLE_ROUTES.overview,
      source
    )
  }

  const exact = legacyExactRoutes[pathname]
  if (exact) return buildTargetHref(exact, source)

  // Segment-aware prefixes for old routes that had dynamic sections.
  if (pathname.startsWith('/dashboard/')) {
    return buildTargetHref(CONSOLE_ROUTES.overview, source)
  }
  if (pathname.startsWith('/models/')) {
    return buildTargetHref(CONSOLE_ROUTES.connectionsCatalog, source)
  }
  if (pathname.startsWith('/usage-logs/')) {
    return buildTargetHref(CONSOLE_ROUTES.logsRequests, source)
  }
  if (pathname.startsWith('/console/')) {
    return buildTargetHref(CONSOLE_ROUTES.overview, source)
  }

  return null
}
