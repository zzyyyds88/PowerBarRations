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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { useSidebarConfig } from '../use-sidebar-config'
import { useSidebarData } from '../use-sidebar-data'

beforeEach(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  useAuthStore.getState().auth.reset()
})

function sidebarFor(admin?: object, user?: object, canConfigure = true) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], {
    SidebarModulesAdmin: admin ? JSON.stringify(admin) : '',
  })
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'alice',
    role: 1,
    permissions: { sidebar_settings: canConfigure },
    sidebar_modules: user ? JSON.stringify(user) : '',
  })
  function Wrapper(props: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        {props.children}
      </QueryClientProvider>
    )
  }
  return renderHook(() => useSidebarConfig(useSidebarData().navGroups), {
    wrapper: Wrapper,
  })
}

type SidebarTestItem = {
  url?: string
  items?: { url?: string }[]
}

function urls(groups: { items: SidebarTestItem[] }[]) {
  return groups.flatMap((group) =>
    group.items.flatMap((item) =>
      item.url ? [item.url] : (item.items ?? []).map((sub) => sub.url)
    )
  )
}

describe('console sidebar visibility', () => {
  it('keeps API Keys visible under legacy personal configurations', () => {
    const { result } = sidebarFor(
      { personal: { enabled: true, personal: true, topup: true } },
      { personal: { enabled: true, personal: true } }
    )
    expect(urls(result.current)).toContain('/keys')
    expect(result.current.some((group) => group.id === 'personal')).toBe(false)
  })

  it('users without sidebar configuration permission retain the full console', () => {
    const { result } = sidebarFor(
      undefined,
      { console: { enabled: false } },
      false
    )
    const visible = urls(result.current)
    expect(visible).toContain('/overview')
    expect(visible).toContain('/keys')
    expect(visible).toContain('/connections/channels')
    expect(visible).toContain('/connections/health')
    expect(visible).toContain('/connections/catalog')
    expect(visible).toContain('/system-settings/site')
  })

  it('hides console destinations when the console section is disabled', () => {
    const { result } = sidebarFor({ console: { enabled: false } })
    const visible = urls(result.current)
    expect(visible).not.toContain('/overview')
    expect(visible).not.toContain('/keys')
    expect(visible).not.toContain('/logs/requests')
    // Admin destinations remain reachable.
    expect(visible).toContain('/connections/channels')
    expect(visible).toContain('/connections/health')
    expect(visible).toContain('/connections/catalog')
    expect(visible).toContain('/routes')
  })

  it('gates each split destination by its own admin module', () => {
    const channelOff = sidebarFor({
      admin: { enabled: true, channel: false, models: true },
    })
    const channelUrls = urls(channelOff.result.current)
    expect(channelUrls).not.toContain('/connections/channels')
    expect(channelUrls).not.toContain('/connections/health')
    expect(channelUrls).toContain('/connections/catalog')
    expect(channelUrls).toContain('/routes')

    const modelsOff = sidebarFor({
      admin: { enabled: true, channel: true, models: false },
    })
    const modelUrls = urls(modelsOff.result.current)
    expect(modelUrls).toContain('/connections/channels')
    expect(modelUrls).toContain('/connections/health')
    expect(modelUrls).not.toContain('/connections/catalog')
    expect(modelUrls).not.toContain('/routes')
  })

  it('gates usage requests and logs separately', () => {
    const logOffAuditOn = sidebarFor({
      console: { enabled: true, log: false, audit: true },
    })
    const visible = urls(logOffAuditOn.result.current)
    expect(visible).not.toContain('/logs/requests')
    expect(visible).not.toContain('/logs/sessions')
    expect(visible).not.toContain('/logs/context')
    expect(visible).toContain('/logs/audit')

    const allOff = sidebarFor({
      console: { enabled: true, log: false, audit: false, task: false },
    })
    expect(urls(allOff.result.current)).not.toContain('/logs/requests')
    expect(urls(allOff.result.current)).not.toContain('/logs/audit')
  })

  it('never exposes Playground or the retired top-level entries', () => {
    const { result } = sidebarFor()
    const visible = urls(result.current)
    expect(visible).not.toContain('/playground')
    expect(visible).not.toContain('/channels')
    expect(visible).not.toContain('/channel-status')
    expect(visible).not.toContain('/models/metadata')
    expect(visible).not.toContain('/usage-logs/audit')
    expect(visible).not.toContain('/system-tasks')
  })

  it('hides Settings with the admin setting module', () => {
    const visible = sidebarFor({ admin: { enabled: true, setting: true } })
    expect(urls(visible.result.current)).toContain('/system-settings/site')

    const hidden = sidebarFor({ admin: { enabled: true, setting: false } })
    expect(urls(hidden.result.current)).not.toContain('/system-settings/site')
  })
})
