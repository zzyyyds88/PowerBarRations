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
/*
PowerBarRations —— 车道运行态 SSE 客户端与双源对账（ui-spec §4、routing-spec §7）

- SSE 帧解析（fetch + ReadableStream 手工解析 event/data 帧，Cookie 自动携带）；
- 断线指数退避重连（带上限与抖动），重连后服务端先推全量快照再推增量；
- 双源对账：SSE 新鲜时 SSE 主导渲染、30s 轮询结果只补 SSE 缺失的车道（对账），
  避免双源交替导致闪烁；SSE 断开/过期时轮询主导，轮询缺失的车道回退最后一帧 SSE。

快照字段形状以 docs/api-spec-v1.md §6.5 为准：时间字段为 RFC3339 字符串（可 null），
亲和为 `affinity` 对象（不为 unix 秒数）。不兼容旧形状。
*/

/** 车道成员运行态（GET /api/lanes/{name}/health 的 members 元素，api-spec §6.5）。 */
export interface LaneMemberHealth {
  /** 成员别名或 channel/upstream_model 标签（后端 memberLabel）。 */
  member: string
  channel: string
  upstream_model: string
  /** 熔断三态：closed | open | half-open（未知值按"非 open"降级展示）。 */
  circuit: string
  consecutive_failures: number
  failure_score?: number
  rolling_success_rate?: number
  /** 冷却截止（RFC3339；null=不在冷却）。 */
  cooldown_until: string | null
  /** 熔断打开到期（RFC3339；仅 circuit 非 closed 时有意义）。 */
  circuit_open_until?: string | null
  last_error_kind?: string
  /** 车道当前成员（亲和/粘性选中的成员）。 */
  current: boolean
  /** 探测占用中（半开探测）。 */
  probing: boolean
  /** 当前是否可被选中（未冷却、熔断非 open，且未被人工停用）。 */
  available: boolean
  /**
   * 该成员是否参与选路（配置态人工开关，恒回；api-spec §6.5）。
   *
   * 与 `available` 是两件事：`available=false` 不蕴含任何故障结论（可能只是冷却），
   * 只有 `enabled=false` 才说明"是运维关的"。被关闭成员的 `circuit` /
   * `cooldown_until` / `consecutive_failures` 仍照实给出，所以排障时要先看这个
   * 字段才能分清"我关的"与"上游挂了"。字段缺席按"参与选路"处理（老后端兼容）。
   */
  enabled?: boolean
}

/** 车道亲和（api-spec §6.5 目标形状：对象 + until RFC3339，可为 null）。 */
export interface LaneAffinity {
  channel: string
  upstream_model: string
  until: string | null
}

/** 一条车道的运行态快照（health 端点与 SSE route-state 帧共用同一形状）。 */
export interface LaneHealthSnapshot {
  lane: string
  source?: string
  mode?: string
  current_member: string
  probe_member: string
  affinity: LaneAffinity | null
  members: LaneMemberHealth[]
  events?: unknown[]
}

/** SSE `route-state` 帧载荷：全量车道快照 + 服务器时间戳。 */
export interface RouteStateFrame {
  ts: string
  lanes: LaneHealthSnapshot[]
}

/** 解析出的一个 SSE 帧。 */
export interface SseFrame {
  event: string
  data: string
}

/** SSE 端点（api-spec §5 前缀说明：/api/v1 为兼容别名，与前端其余调用同一前缀）。 */
export const ROUTE_EVENTS_SSE_URL = '/api/v1/route-events'

/** 退避参数：1s 起指数翻倍，封顶 30s，附加最多 25% 抖动（防多标签同时重连惊群）。 */
export const RECONNECT_BASE_MS = 1_000
export const RECONNECT_MAX_MS = 30_000
export const RECONNECT_JITTER_RATIO = 0.25

/** SSE 快照的"新鲜"窗口：服务端每 ~1s 推一帧，超过该窗口视为失联。 */
export const SSE_SNAPSHOT_STALE_MS = 10_000

/**
 * 重连退避时长（纯函数，便于测试）：第 attempt 次重连（从 0 计）。
 * `random` 注入以便测试确定性；默认按 jitterRatio 加 0~25% 抖动，总时长不超上限。
 */
export function reconnectDelayMs(
  attempt: number,
  options?: {
    baseMs?: number
    maxMs?: number
    jitterRatio?: number
    random?: () => number
  }
): number {
  const baseMs = options?.baseMs ?? RECONNECT_BASE_MS
  const maxMs = options?.maxMs ?? RECONNECT_MAX_MS
  const jitterRatio = options?.jitterRatio ?? RECONNECT_JITTER_RATIO
  const random = options?.random ?? Math.random
  const capped = Math.min(maxMs, baseMs * 2 ** Math.max(0, attempt))
  const jitter = 1 + random() * jitterRatio
  return Math.min(maxMs, Math.round(capped * jitter))
}

/**
 * 增量 SSE 帧解析器（fetch + ReadableStream 场景，配合 ui-spec §4"不用 EventSource"）。
 *
 * 处理任意分块切割：行尾 \r\n 或 \n、跨 chunk 的半行、多行 data、
 * `:` 注释/心跳行、event 名默认 "message"。
 */
export function createSseFrameParser(): {
  feed: (chunk: string) => SseFrame[]
  /** 流结束时取回未以空行收尾的最后一段（服务器异常断开时不丢最后一帧）。 */
  flush: () => SseFrame[]
} {
  let buffer = ''
  let eventName = 'message'
  let dataLines: string[] = []
  const pending: SseFrame[] = []

  const takeLine = (line: string) => {
    if (line === '') {
      if (dataLines.length > 0) {
        pending.push({ event: eventName, data: dataLines.join('\n') })
      }
      eventName = 'message'
      dataLines = []
      return
    }
    if (line.startsWith(':')) return // 注释/心跳
    const colon = line.indexOf(':')
    const field = colon === -1 ? line : line.slice(0, colon)
    let value = colon === -1 ? '' : line.slice(colon + 1)
    if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'event') eventName = value
    else if (field === 'data') dataLines.push(value)
  }

  return {
    feed(chunk: string) {
      buffer += chunk
      let idx = buffer.indexOf('\n')
      while (idx !== -1) {
        const line = buffer.slice(0, idx).replace(/\r$/, '')
        buffer = buffer.slice(idx + 1)
        takeLine(line)
        idx = buffer.indexOf('\n')
      }
      return pending.splice(0, pending.length)
    },
    flush() {
      if (buffer !== '') {
        const line = buffer.replace(/\r$/, '')
        buffer = ''
        takeLine(line)
      }
      if (dataLines.length > 0) {
        pending.push({ event: eventName, data: dataLines.join('\n') })
        eventName = 'message'
        dataLines = []
      }
      return pending.splice(0, pending.length)
    },
  }
}

function isLaneHealthSnapshot(value: unknown): value is LaneHealthSnapshot {
  if (typeof value !== 'object' || value === null) return false
  const v = value as Record<string, unknown>
  return typeof v.lane === 'string' && Array.isArray(v.members)
}

/**
 * 解析 `route-state` 帧的 JSON 载荷（{ts, lanes:[HealthSnapshot...]}）。
 * 宽松校验：形状不符（后端旧版本/半截数据）返回 null，由调用方忽略该帧，
 * 绝不因单帧异常中断连接。
 */
export function parseRouteStateFrame(frame: SseFrame): RouteStateFrame | null {
  if (frame.event !== 'route-state') return null
  let raw: unknown
  try {
    raw = JSON.parse(frame.data)
  } catch {
    return null
  }
  if (typeof raw !== 'object' || raw === null) return null
  const payload = raw as { ts?: unknown; lanes?: unknown }
  if (typeof payload.ts !== 'string' || !Array.isArray(payload.lanes)) {
    return null
  }
  const lanes = payload.lanes.filter(isLaneHealthSnapshot)
  if (lanes.length !== payload.lanes.length) return null
  return { ts: payload.ts, lanes }
}

/** SSE 与轮询双源的运行态合并结果。 */
export interface ResolvedLaneRuntime {
  /** 车道名 → 生效快照。 */
  byLane: Map<string, LaneHealthSnapshot>
  /** 本次渲染是否有 SSE 主导（轮询兜底主导时为 false）。 */
  sseDriven: boolean
}

/**
 * 双源对账（纯函数）：
 * - SSE 连接活着且快照新鲜（now - sseReceivedAt <= staleMs）→ SSE 主导；
 *   轮询结果只为"SSE 里没有的车道"兜底（对账），不覆盖 SSE，避免闪烁。
 * - SSE 断开/过期 → 轮询主导；轮询还没覆盖的车道回退最后一帧 SSE，避免空洞。
 */
export function resolveLaneSnapshots(params: {
  sseLanes: LaneHealthSnapshot[] | null
  sseReceivedAt: number | null
  sseConnected: boolean
  pollLanes: LaneHealthSnapshot[] | null
  now: number
  staleMs?: number
}): ResolvedLaneRuntime {
  const staleMs = params.staleMs ?? SSE_SNAPSHOT_STALE_MS
  const sseFresh =
    params.sseConnected &&
    params.sseLanes !== null &&
    params.sseReceivedAt !== null &&
    params.now - params.sseReceivedAt <= staleMs

  const toMap = (lanes: LaneHealthSnapshot[] | null) => {
    const map = new Map<string, LaneHealthSnapshot>()
    for (const lane of lanes ?? []) map.set(lane.lane, lane)
    return map
  }

  const byLane = toMap(sseFresh ? params.sseLanes : params.pollLanes)
  const fallback = toMap(sseFresh ? params.pollLanes : params.sseLanes)
  for (const [name, snap] of fallback) {
    if (!byLane.has(name)) byLane.set(name, snap)
  }
  return { byLane, sseDriven: sseFresh }
}

export type RouteEventStatus = 'connecting' | 'open' | 'reconnecting' | 'closed'

export interface RouteEventStreamOptions {
  /** 收到 route-state 帧（一次为该连接当前全量车道的快照）。 */
  onLanes: (lanes: LaneHealthSnapshot[], ts: string) => void
  /** 连接状态变化（指示与排障用；渲染不依赖它也能工作）。 */
  onStatus?: (status: RouteEventStatus) => void
  /** 每次（重）连取的鉴权头（会话 Cookie 由 credentials 携带，这里补 Bearer）。 */
  headers?: () => Record<string, string> | Promise<Record<string, string>>
  url?: string
  fetchImpl?: typeof fetch
  /** 注入等待函数以便测试不 sleep。 */
  sleep?: (ms: number, signal: AbortSignal) => Promise<void>
  /** 覆盖退避函数（测试可控）。 */
  delayFor?: (attempt: number) => number
  random?: () => number
}

export interface RouteEventStream {
  close: () => void
}

const defaultSleep = (ms: number, signal: AbortSignal): Promise<void> =>
  new Promise((resolve) => {
    const timer = setTimeout(resolve, ms)
    signal.addEventListener(
      'abort',
      () => {
        clearTimeout(timer)
        resolve()
      },
      { once: true }
    )
  })

/**
 * 建立 `GET /api/route-events` SSE 长连接（ui-spec §4：fetch + ReadableStream，
 * 不用 EventSource；`credentials:'include'` 携带会话 Cookie；路径与 api 实例
 * baseURL 口径一致——baseURL 为空、同源相对路径）。
 *
 * 断线自动重连（指数退避 + 上限 + 抖动）；服务端在建连时先推全量快照，
 * 天然满足"重连后先取一次快照再接受增量"。close() 后不再重连。
 */
export function openRouteEventStream(
  options: RouteEventStreamOptions
): RouteEventStream {
  const controller = new AbortController()
  let closed = false
  const fetchImpl: typeof fetch = options.fetchImpl ?? globalThis.fetch
  const sleep = options.sleep ?? defaultSleep
  const url = options.url ?? ROUTE_EVENTS_SSE_URL

  const setStatus = (status: RouteEventStatus) => options.onStatus?.(status)

  const emitFrame = (frame: SseFrame): boolean => {
    const parsed = parseRouteStateFrame(frame)
    if (!parsed) return false
    options.onLanes(parsed.lanes, parsed.ts)
    return true
  }

  const run = async () => {
    let attempt = 0
    while (!closed) {
      setStatus(attempt === 0 ? 'connecting' : 'reconnecting')
      let receivedFrame = false
      try {
        const headers: Record<string, string> = {
          Accept: 'text/event-stream',
          ...(await options.headers?.()),
        }
        const response = await fetchImpl(url, {
          method: 'GET',
          headers,
          credentials: 'include',
          cache: 'no-store',
          signal: controller.signal,
        })
        if (!response.ok || !response.body) {
          throw new Error(`SSE connect failed: HTTP ${response.status}`)
        }
        setStatus('open')
        const parser = createSseFrameParser()
        const reader = response.body.getReader()
        const decoder = new TextDecoder()
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          for (const frame of parser.feed(
            decoder.decode(value, { stream: true })
          )) {
            receivedFrame = emitFrame(frame) || receivedFrame
          }
        }
        for (const frame of parser.flush()) {
          receivedFrame = emitFrame(frame) || receivedFrame
        }
      } catch {
        if (closed || controller.signal.aborted) return
      }
      if (closed || controller.signal.aborted) return
      // 成功收到过帧的连接断开后从头退避；从未收到帧（服务器不可用）持续加倍到上限。
      if (receivedFrame) attempt = 0
      const delay =
        options.delayFor?.(attempt) ??
        reconnectDelayMs(attempt, { random: options.random })
      attempt += 1
      await sleep(delay, controller.signal)
    }
  }

  void run()
  return {
    close() {
      closed = true
      controller.abort()
      setStatus('closed')
    },
  }
}
