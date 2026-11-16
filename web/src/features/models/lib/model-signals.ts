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
import { useMemo, useState } from 'react'

import { searchChannels } from '@/features/channels/api'
import {
  effectivePriceAt,
  type PricedChannelModel,
} from '@/features/channels/lib/channel-prices'
import { parseChannelSettings } from '@/features/channels/lib/channel-utils'
import { getPBRModelStatus } from '@/features/dashboard/model-status-api'

import type { Model } from '../types'
import { channelRouteKeys, matchesName } from './channel-price'

// 模型目录的只读「价格 / 健康」信号（ui-spec §6 模型目录）。
//
// - 价格：声明该模型的渠道 `pbr_prices` 聚合；峰谷价按**当前时刻**取生效价，
//   多条渠道给 min–max 区间。价格真相仍在渠道 Sheet，模型目录不写。
// - 健康：渠道声明/映射出的上游模型名，去重后汇总 `/api/model-status` 的
//   24h 请求数与成功率。映射链按 `model_mapping` 的 source → target 解析。
//
// 两路数据都是只读投影；没有样本时返回 null，界面显示 `—`，不伪造 0。

const CHANNEL_PAGE_SIZE = 1000
const STATUS_WINDOW_SECONDS = 24 * 60 * 60
const STALE_TIME_MS = 60 * 1000

export interface PriceDimension {
  min: number
  max: number
}

export interface ModelPriceSummary {
  input: PriceDimension | null
  output: PriceDimension | null
  cacheRead: PriceDimension | null
  cacheWrite: PriceDimension | null
  channelCount: number
}

export interface ModelHealthSummary {
  requests: number
  successRate: number
}

export interface ModelSignals {
  price: ModelPriceSummary | null
  health: ModelHealthSummary | null
}

type PriceAccumulator = {
  input: number[]
  output: number[]
  cache_read: number[]
  cache_write: number[]
  channelIds: Set<number>
}

function parseMapping(raw: string | null | undefined): Record<string, string> {
  if (!raw?.trim()) return {}
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return {}
    }
    const mapping: Record<string, string> = {}
    for (const [key, value] of Object.entries(parsed)) {
      if (typeof value !== 'string') continue
      const target = value.trim()
      if (target) mapping[key.trim()] = target
    }
    return mapping
  } catch {
    return {}
  }
}

function toDimension(values: number[]): PriceDimension | null {
  if (values.length === 0) return null
  return { min: Math.min(...values), max: Math.max(...values) }
}

function buildSignals(
  models: Model[],
  channels: Awaited<ReturnType<typeof searchChannels>>['items'],
  statusItems: Awaited<ReturnType<typeof getPBRModelStatus>>['items'],
  now: Date
): Map<string, ModelSignals> {
  const statusByModel = new Map(statusItems.map((item) => [item.model, item]))
  const channelInfo = channels.map((channel) => {
    const settings = parseChannelSettings(channel.setting)
    return {
      id: channel.id,
      name: channel.name,
      keys: channelRouteKeys(channel),
      mapping: parseMapping(channel.model_mapping),
      prices: (settings.pbr_prices ?? []) as PricedChannelModel[],
    }
  })

  const result = new Map<string, ModelSignals>()
  for (const model of models) {
    const rule = model.name_rule ?? 0
    const priceAcc: PriceAccumulator = {
      input: [],
      output: [],
      cache_read: [],
      cache_write: [],
      channelIds: new Set(),
    }
    const upstreamModels = new Set<string>()

    for (const channel of channelInfo) {
      const servesModel = channel.keys.some((key) =>
        matchesName(key, model.model_name, rule)
      )
      if (!servesModel) continue

      // 健康：该渠道实际会打到哪些上游模型名。规则命中的每个具体请求名都要
      // 过一遍 model_mapping，再对状态表去重汇总。
      for (const key of channel.keys) {
        if (!matchesName(key, model.model_name, rule)) continue
        const target = channel.mapping[key] ?? key
        if (target) upstreamModels.add(target)
      }

      // 价格：价格行的 model 是请求模型名；规则条目匹配其覆盖的具体行。
      for (const price of channel.prices) {
        if (
          !price?.model ||
          !matchesName(price.model, model.model_name, rule)
        ) {
          continue
        }
        priceAcc.channelIds.add(channel.id)
        const effective = effectivePriceAt(price, now)
        priceAcc.input.push(effective.input)
        priceAcc.output.push(effective.output)
        priceAcc.cache_read.push(effective.cache_read)
        priceAcc.cache_write.push(effective.cache_write)
      }
    }

    const price: ModelPriceSummary | null =
      priceAcc.channelIds.size === 0
        ? null
        : {
            input: toDimension(priceAcc.input),
            output: toDimension(priceAcc.output),
            cacheRead: toDimension(priceAcc.cache_read),
            cacheWrite: toDimension(priceAcc.cache_write),
            channelCount: priceAcc.channelIds.size,
          }

    let requests = 0
    let successes = 0
    for (const upstream of upstreamModels) {
      const status = statusByModel.get(upstream)
      if (!status) continue
      requests += status.requests
      successes += status.successes
    }
    const health: ModelHealthSummary | null =
      requests > 0 ? { requests, successRate: successes / requests } : null

    result.set(model.model_name, { price, health })
  }
  return result
}

export function useModelCatalogSignals(models: Model[]) {
  // 固定挂载时刻的 24h 窗口：查询键在页面生命周期内保持稳定。
  const [window] = useState(() => {
    const to = Math.floor(Date.now() / 1000)
    return { from: to - STATUS_WINDOW_SECONDS, to }
  })

  const channelsQuery = useQuery({
    queryKey: ['models', 'channel-signals'],
    queryFn: () => searchChannels({ p: 1, page_size: CHANNEL_PAGE_SIZE }),
    staleTime: STALE_TIME_MS,
  })
  const statusQuery = useQuery({
    queryKey: ['models', 'model-status-signals', window.from, window.to],
    queryFn: () => getPBRModelStatus(window),
    staleTime: STALE_TIME_MS,
    refetchOnWindowFocus: false,
  })

  return useMemo(() => {
    if (!channelsQuery.data || !statusQuery.data) {
      return new Map<string, ModelSignals>()
    }
    return buildSignals(
      models,
      channelsQuery.data.items,
      statusQuery.data.items,
      new Date()
    )
  }, [models, channelsQuery.data, statusQuery.data])
}

export function formatPriceValue(value: number): string {
  if (!Number.isFinite(value)) return '—'
  return `¥${new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 4,
  }).format(value)}`
}

export function formatPriceRange(dimension: PriceDimension | null): string {
  if (!dimension) return '—'
  if (dimension.min === dimension.max) return formatPriceValue(dimension.min)
  return `${formatPriceValue(dimension.min)}–${formatPriceValue(dimension.max)}`
}
