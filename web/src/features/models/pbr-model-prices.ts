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
import { useQuery } from '@tanstack/react-query'

import { getSystemOptions } from '@/features/system-settings/api'

// PBR 全局默认单价表（options 表的 PBRModelPrices）。
//
// 渠道级上游单价优先；没有渠道价的模型在这里显示默认价。人民币/百万 token。

export type PBRUnitPrice = {
  model: string
  input: number
  output: number
  cache_read: number
  cache_write: number
}

export function usePBRModelPrices() {
  return useQuery({
    queryKey: ['pbr-model-prices'],
    queryFn: async (): Promise<Map<string, PBRUnitPrice>> => {
      const res = await getSystemOptions()
      const option = (res.data ?? []).find(
        (item) => item.key === 'PBRModelPrices'
      )
      let list: PBRUnitPrice[] = []
      try {
        const parsed = JSON.parse(option?.value ?? '[]')
        if (Array.isArray(parsed)) list = parsed as PBRUnitPrice[]
      } catch {
        list = []
      }
      const map = new Map<string, PBRUnitPrice>()
      for (const item of list) {
        if (item && typeof item.model === 'string' && item.model !== '') {
          map.set(item.model, item)
        }
      }
      return map
    },
    staleTime: 60 * 1000,
  })
}
