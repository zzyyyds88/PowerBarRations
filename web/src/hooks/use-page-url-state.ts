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
import { useLocation, useNavigate, useSearch } from '@tanstack/react-router'
import { useCallback } from 'react'

import type { NavigateFn } from './use-table-url-state'

/**
 * Route-agnostic URL state for list pages.
 *
 * 旧实现用 `getRouteApi('/_authenticated/<old-path>')` 把表格钉死在具体路由上，
 * 重写信息架构时换 URL 会同时打断页面与测试。这里只读当前 URL 的 search 并
 * 对当前路径做 replace/push，表格组件因此可以在任意工作区路由下复用。
 */
export function usePageUrlState(): {
  search: Record<string, unknown>
  navigate: NavigateFn
} {
  const search = useSearch({ strict: false }) as Record<string, unknown>
  const routerNavigate = useNavigate()
  const { pathname } = useLocation()

  const navigate = useCallback<NavigateFn>(
    (opts) => {
      void routerNavigate({
        to: pathname,
        search: opts.search,
        replace: opts.replace,
      } as never)
    },
    [pathname, routerNavigate]
  )

  return { search, navigate }
}
