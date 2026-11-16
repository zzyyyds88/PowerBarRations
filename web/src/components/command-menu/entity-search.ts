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
/**
 * Command-palette entity search (ui-spec §12).
 *
 * Read-only over existing management endpoints — no search backend is added:
 *   channels  GET /api/v1/channels
 *   keys      GET /api/v1/keys
 *   models    GET /api/v1/models
 *   lanes     GET /api/v1/lane-summaries
 *   request   GET /api/v1/logs/{id}
 *
 * The lists are cursor-paginated (limit 100). `fetchAllPages` follows
 * `next_cursor` a bounded number of times so one palette open cannot walk an
 * unbounded collection. Matching is a case-insensitive substring over the
 * display label plus optional keywords, then the caller renders at most five
 * rows per group.
 */
import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'

export type CommandEntityKind = 'channel' | 'key' | 'model' | 'lane' | 'request'

export type CommandEntityTarget =
  | { kind: 'channel'; name: string }
  | { kind: 'key'; name: string }
  | { kind: 'model'; model: string }
  | { kind: 'lane'; model: string }
  | {
      kind: 'request'
      id: number
      lane: string
      channel: string
      key: string
      model: string
      /** Unix seconds; the logs deep link narrows to this second. */
      ts: number
    }

export interface CommandEntity {
  id: string
  kind: CommandEntityKind
  label: string
  /** Data-only hint parts (type / prefix / source / mode); translated labels
   * such as "disabled" are added by the renderer, never stored here. */
  hintParts?: string[]
  /** Entity status flag, rendered with `t('Disabled')` / `t('Enabled')`. */
  disabled?: boolean
  /** Request outcome, rendered with `t('Success')` / `t('Failed')`. */
  success?: boolean
  keywords?: string
  target: CommandEntityTarget
}

export interface CommandEntityGroup {
  id: CommandEntityKind
  /** Already translated by the caller through `t(groupLabelKey(id))`. */
  items: CommandEntity[]
}

interface ListResponse<T> {
  items?: T[]
  next_cursor?: string | null
}

interface ChannelItem {
  name?: string
  type?: string
  enabled?: boolean
}

interface KeyItem {
  name?: string
  enabled?: boolean
  key_prefix?: string
  lane_policy?: { mode?: string }
}

interface ModelItem {
  model?: string
  source?: string
  routable?: boolean
}

interface LaneItem {
  name?: string
  mode?: string
  enabled?: boolean
}

interface RequestLogItem {
  id?: number
  ts?: string
  lane?: string
  channel?: string
  key_name?: string
  request_model?: string
  upstream_model?: string
  success?: boolean
}

const PAGE_LIMIT = 100
/** Bounded cursor walking: 5 pages = 500 rows per entity kind. */
const MAX_PAGES = 5
const MAX_RESULTS_PER_GROUP = 5
const SEARCH_STALE_MS = 60 * 1000

async function fetchAllPages<T>(
  path: string,
  limit = PAGE_LIMIT,
  maxPages = MAX_PAGES
): Promise<T[]> {
  const items: T[] = []
  let cursor: string | undefined
  for (let page = 0; page < maxPages; page += 1) {
    const res = await api.get<ListResponse<T>>(path, {
      params: { limit, ...(cursor ? { cursor } : {}) },
    })
    items.push(...(res.data.items ?? []))
    const next = res.data.next_cursor
    if (!next) break
    cursor = next
  }
  return items
}

function includesTerm(term: string, ...values: Array<string | undefined>) {
  return values.some((value) => value?.toLowerCase().includes(term))
}

function toChannels(items: ChannelItem[]): CommandEntity[] {
  return items.flatMap((item) => {
    if (!item.name) return []
    return [
      {
        id: `channel:${item.name}`,
        kind: 'channel' as const,
        label: item.name,
        hintParts: item.type ? [item.type] : [],
        disabled: item.enabled === false,
        keywords: item.type,
        target: { kind: 'channel' as const, name: item.name },
      },
    ]
  })
}

function toKeys(items: KeyItem[]): CommandEntity[] {
  return items.flatMap((item) => {
    if (!item.name) return []
    return [
      {
        id: `key:${item.name}`,
        kind: 'key' as const,
        label: item.name,
        hintParts: [item.key_prefix, item.lane_policy?.mode].filter(
          (value): value is string => Boolean(value)
        ),
        keywords: item.key_prefix,
        target: { kind: 'key' as const, name: item.name },
      },
    ]
  })
}

function toModels(items: ModelItem[]): CommandEntity[] {
  return items.flatMap((item) => {
    if (!item.model) return []
    return [
      {
        id: `model:${item.model}`,
        kind: 'model' as const,
        label: item.model,
        hintParts: item.source ? [item.source] : [],
        keywords: item.source,
        target: { kind: 'model' as const, model: item.model },
      },
    ]
  })
}

function toLanes(items: LaneItem[]): CommandEntity[] {
  return items.flatMap((item) => {
    if (!item.name) return []
    return [
      {
        id: `lane:${item.name}`,
        kind: 'lane' as const,
        label: item.name,
        hintParts: item.mode ? [item.mode] : [],
        disabled: item.enabled === false,
        keywords: item.mode,
        target: { kind: 'lane' as const, model: item.name },
      },
    ]
  })
}

function selectMatches(
  entities: CommandEntity[],
  term: string
): CommandEntity[] {
  const normalized = term.toLowerCase()
  const matched = entities.filter((entity) =>
    includesTerm(normalized, entity.label, entity.keywords)
  )
  return matched.slice(0, MAX_RESULTS_PER_GROUP)
}

function requestEntity(log: RequestLogItem | null): CommandEntity | null {
  if (!log?.id) return null
  const ts = log.ts ? Math.floor(new Date(log.ts).getTime() / 1000) : 0
  return {
    id: `request:${log.id}`,
    kind: 'request',
    label: `#${log.id}`,
    hintParts: [log.lane, log.request_model].filter((value): value is string =>
      Boolean(value)
    ),
    success: log.success === true,
    target: {
      kind: 'request',
      id: log.id,
      lane: log.lane ?? '',
      channel: log.channel ?? '',
      key: log.key_name ?? '',
      model: log.request_model ?? '',
      ts,
    },
  }
}

/** Numeric request ids are the PBR log primary key (`pbr-logs-*.csv`). */
export function parseRequestId(term: string): number | null {
  const trimmed = term.trim()
  if (!/^\d+$/.test(trimmed)) return null
  const id = Number(trimmed)
  return Number.isSafeInteger(id) && id > 0 ? id : null
}

export function useCommandEntitySearch(
  term: string,
  enabled: boolean
): { groups: CommandEntityGroup[]; isLoading: boolean } {
  const requestId = parseRequestId(term)
  const shouldSearch = enabled && term.trim().length > 0

  const channels = useQuery({
    queryKey: ['command-search', 'channels'],
    queryFn: () => fetchAllPages<ChannelItem>('/api/v1/channels'),
    enabled: shouldSearch,
    staleTime: SEARCH_STALE_MS,
  })
  const keys = useQuery({
    queryKey: ['command-search', 'keys'],
    queryFn: () => fetchAllPages<KeyItem>('/api/v1/keys'),
    enabled: shouldSearch,
    staleTime: SEARCH_STALE_MS,
  })
  const models = useQuery({
    queryKey: ['command-search', 'models'],
    queryFn: () => fetchAllPages<ModelItem>('/api/v1/models', PAGE_LIMIT, 1),
    enabled: shouldSearch,
    staleTime: SEARCH_STALE_MS,
  })
  const lanes = useQuery({
    queryKey: ['command-search', 'lanes'],
    queryFn: () =>
      fetchAllPages<LaneItem>('/api/v1/lane-summaries', PAGE_LIMIT, 1),
    enabled: shouldSearch,
    staleTime: SEARCH_STALE_MS,
  })
  const request = useQuery({
    queryKey: ['command-search', 'request', requestId],
    queryFn: async () => {
      const res = await api.get<RequestLogItem>(
        `/api/v1/logs/${requestId as number}`
      )
      return res.data
    },
    enabled: shouldSearch && requestId !== null,
    staleTime: SEARCH_STALE_MS,
    retry: false,
  })

  const groups: CommandEntityGroup[] = []
  // Disabled queries keep their cached data; without this guard an empty
  // input would match every cached entity.
  if (!shouldSearch) return { groups, isLoading: false }
  const channelItems = selectMatches(toChannels(channels.data ?? []), term)
  if (channelItems.length > 0) {
    groups.push({ id: 'channel', items: channelItems })
  }
  const keyItems = selectMatches(toKeys(keys.data ?? []), term)
  if (keyItems.length > 0) groups.push({ id: 'key', items: keyItems })
  const modelItems = selectMatches(toModels(models.data ?? []), term)
  if (modelItems.length > 0) groups.push({ id: 'model', items: modelItems })
  const laneItems = selectMatches(toLanes(lanes.data ?? []), term)
  if (laneItems.length > 0) groups.push({ id: 'lane', items: laneItems })
  const requestItem = requestEntity(request.data ?? null)
  if (requestItem) groups.push({ id: 'request', items: [requestItem] })

  const isLoading =
    shouldSearch &&
    (channels.isFetching ||
      keys.isFetching ||
      models.isFetching ||
      lanes.isFetching ||
      (requestId !== null && request.isFetching))

  return { groups, isLoading }
}
