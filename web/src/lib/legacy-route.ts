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
const legacyOrigin = 'https://legacy-route.invalid'

// 上游 new-api 旧控制台（/console/*）到新控制台的映射。
// 只保留 PBR 仍然存在的目标页；多用户/计费类旧路径
// （subscription/redemption/user/personal/topup）不再单列，统一由下方
// `/console/` 前缀兜底到 /dashboard，避免跳到已删除的路由。
const legacyConsoleRoutes: Record<string, string> = {
  '/console': '/dashboard',
  '/console/models': '/models',
  '/console/channel': '/channels',
  '/console/token': '/keys',
  '/console/playground': '/playground',
  '/console/log': '/usage-logs',
  '/console/midjourney': '/usage-logs/drawing',
  '/console/task': '/usage-logs/task',
}

// 旧设置页 tab → 新设置页子节。仅映射仍存在的 sections
// （content/models/operations/site），计费与安全子节已随 W7 删除。
const legacySettingsTabs: Record<string, string> = {
  operation: '/system-settings/operations/behavior',
  dashboard: '/system-settings/content/dashboard',
  chats: '/system-settings/content/chat',
  drawing: '/system-settings/content/drawing',
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
  if (pathname === '/console/chat') {
    return buildTargetHref('/dashboard', source)
  }
  if (pathname.startsWith('/console/chat/')) {
    const chatID = pathname.slice('/console/chat/'.length)
    return buildTargetHref(chatID ? `/chat/${chatID}` : '/dashboard', source)
  }

  const target = legacyConsoleRoutes[pathname]
  if (target) return buildTargetHref(target, source)
  if (pathname.startsWith('/console/')) {
    return buildTargetHref('/dashboard', source)
  }

  return null
}
