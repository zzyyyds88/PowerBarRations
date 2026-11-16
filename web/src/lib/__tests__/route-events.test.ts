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
车道运行态 lib 单测（ui-spec §4 / routing-spec §7）：
SSE 帧解析、route-state 载荷校验、退避重连节奏、双源对账合并、
openRouteEventStream 的重连与关闭行为（全部注入可控边界，不 sleep）。
*/
import { describe, expect, test, vi } from 'vitest'

import {
  createSseFrameParser,
  openRouteEventStream,
  parseRouteStateFrame,
  reconnectDelayMs,
  resolveLaneSnapshots,
  type LaneHealthSnapshot,
  type SseFrame,
} from '../route-events'

function frame(event: string, data: unknown): SseFrame {
  return { event, data: JSON.stringify(data) }
}

function snapshot(
  lane: string,
  extra?: Partial<LaneHealthSnapshot>
): LaneHealthSnapshot {
  return {
    lane,
    current_member: '',
    probe_member: '',
    affinity: null,
    members: [],
    ...extra,
  }
}

describe('createSseFrameParser', () => {
  test('解析单个 event/data 帧（标准空行分隔）', () => {
    const parser = createSseFrameParser()
    const frames = parser.feed('event: route-state\ndata: {"ts":"t"}\n\n')
    expect(frames).toEqual([{ event: 'route-state', data: '{"ts":"t"}' }])
  })

  test('跨 chunk 的半行与半帧能续接', () => {
    const parser = createSseFrameParser()
    expect(parser.feed('event: route-st')).toEqual([])
    expect(parser.feed('ate\nda')).toEqual([])
    expect(parser.feed('ta: {"a":1}\n')).toEqual([])
    expect(parser.feed('\n')).toEqual([
      { event: 'route-state', data: '{"a":1}' },
    ])
  })

  test('CRLF 行尾与 data 值里的冒号', () => {
    const parser = createSseFrameParser()
    const frames = parser.feed(
      'event: route-state\r\ndata: {"u":"http://x:8080/a"}\r\n\r\n'
    )
    expect(frames).toEqual([
      { event: 'route-state', data: '{"u":"http://x:8080/a"}' },
    ])
  })

  test('多行 data 以换行拼接；注释/心跳行忽略；event 缺省为 message', () => {
    const parser = createSseFrameParser()
    const frames = parser.feed(
      ': keep-alive\ndata: line1\ndata: line2\n\ndata: plain\n\n'
    )
    expect(frames).toEqual([
      { event: 'message', data: 'line1\nline2' },
      { event: 'message', data: 'plain' },
    ])
  })

  test('流异常结束时 flush 取回未以空行收尾的最后一段', () => {
    const parser = createSseFrameParser()
    expect(parser.feed('event: route-state\ndata: {"a":1}')).toEqual([])
    expect(parser.flush()).toEqual([{ event: 'route-state', data: '{"a":1}' }])
  })
})

describe('parseRouteStateFrame', () => {
  test('接受 {ts,lanes} 全量快照帧（api-spec §6.5 形状）', () => {
    const parsed = parseRouteStateFrame(
      frame('route-state', {
        ts: '2026-09-19T12:00:00Z',
        lanes: [snapshot('model-1')],
      })
    )
    expect(parsed).not.toBeNull()
    expect(parsed?.lanes[0]?.lane).toBe('model-1')
  })

  test('非 route-state 事件名被忽略', () => {
    expect(
      parseRouteStateFrame(frame('ping', { ts: 'x', lanes: [] }))
    ).toBeNull()
  })

  test('坏 JSON、缺字段、members 非数组的帧都安全返回 null', () => {
    expect(
      parseRouteStateFrame({ event: 'route-state', data: '{not json' })
    ).toBeNull()
    expect(parseRouteStateFrame(frame('route-state', { lanes: [] }))).toBeNull()
    expect(
      parseRouteStateFrame(
        frame('route-state', { ts: 't', lanes: [{ lane: 1 }] })
      )
    ).toBeNull()
  })
})

describe('reconnectDelayMs（指数退避 + 上限 + 抖动）', () => {
  test('无随机时按 base*2^attempt 指数增长', () => {
    const random = () => 0
    expect(reconnectDelayMs(0, { random })).toBe(1_000)
    expect(reconnectDelayMs(1, { random })).toBe(2_000)
    expect(reconnectDelayMs(3, { random })).toBe(8_000)
  })

  test('封顶 30s，不再无限翻倍', () => {
    const random = () => 0
    expect(reconnectDelayMs(10, { random })).toBe(30_000)
    expect(reconnectDelayMs(30, { random })).toBe(30_000)
  })

  test('抖动最多 +25% 且不越上限', () => {
    expect(reconnectDelayMs(0, { random: () => 1 })).toBe(1_250)
    expect(reconnectDelayMs(10, { random: () => 1 })).toBe(30_000)
  })

  test('attempt 为负按 0 处理', () => {
    expect(reconnectDelayMs(-2, { random: () => 0 })).toBe(1_000)
  })
})

describe('resolveLaneSnapshots（双源对账）', () => {
  const now = 1_000_000

  test('SSE 新鲜时 SSE 主导；轮询仅补 SSE 缺失的车道，不覆盖 SSE', () => {
    const sseLane = snapshot('model-1', { current_member: 'from-sse' })
    const pollSame = snapshot('model-1', { current_member: 'from-poll' })
    const pollOnly = snapshot('model-2')
    const result = resolveLaneSnapshots({
      sseLanes: [sseLane],
      sseReceivedAt: now - 1_000,
      sseConnected: true,
      pollLanes: [pollSame, pollOnly],
      now,
    })
    expect(result.sseDriven).toBe(true)
    expect(result.byLane.get('model-1')?.current_member).toBe('from-sse')
    expect(result.byLane.get('model-2')).toBeDefined()
  })

  test('SSE 断开时轮询主导（同一车道不混渲染）', () => {
    const result = resolveLaneSnapshots({
      sseLanes: [snapshot('model-1', { current_member: 'stale-sse' })],
      sseReceivedAt: now - 1_000,
      sseConnected: false,
      pollLanes: [snapshot('model-1', { current_member: 'fresh-poll' })],
      now,
    })
    expect(result.sseDriven).toBe(false)
    expect(result.byLane.get('model-1')?.current_member).toBe('fresh-poll')
  })

  test('SSE 连着但快照过期（超过新鲜窗口）视为失联，轮询主导', () => {
    const result = resolveLaneSnapshots({
      sseLanes: [snapshot('model-1', { current_member: 'stale-sse' })],
      sseReceivedAt: now - 60_000,
      sseConnected: true,
      pollLanes: [snapshot('model-1', { current_member: 'fresh-poll' })],
      now,
      staleMs: 10_000,
    })
    expect(result.sseDriven).toBe(false)
    expect(result.byLane.get('model-1')?.current_member).toBe('fresh-poll')
  })

  test('轮询还没返回时回退最后一帧 SSE，避免空洞', () => {
    const result = resolveLaneSnapshots({
      sseLanes: [snapshot('model-1', { current_member: 'last-sse' })],
      sseReceivedAt: now - 5_000,
      sseConnected: false,
      pollLanes: null,
      now,
    })
    expect(result.byLane.get('model-1')?.current_member).toBe('last-sse')
  })
})

// —— openRouteEventStream：可控 fetch/ReadableStream/sleep，不依赖真实计时器 ——

function makeStreamBody() {
  const chunks: string[] = []
  let waiting: ((v: { done: boolean; value?: Uint8Array }) => void) | null =
    null
  const encoder = new TextEncoder()
  const body = {
    getReader: () => ({
      read: () =>
        new Promise<{ done: boolean; value?: Uint8Array }>((resolve) => {
          const chunk = chunks.shift()
          if (chunk !== undefined) {
            resolve({ done: false, value: encoder.encode(chunk) })
            return
          }
          waiting = resolve
        }),
    }),
  }
  return {
    body,
    push(text: string) {
      const resolve = waiting
      waiting = null
      if (resolve) resolve({ done: false, value: encoder.encode(text) })
      else chunks.push(text)
    },
    close() {
      const resolve = waiting
      waiting = null
      resolve?.({ done: true })
    },
  }
}

const routeStateChunk = (lanes: LaneHealthSnapshot[]) =>
  `event: route-state\ndata: ${JSON.stringify({
    ts: '2026-09-19T12:00:00Z',
    lanes,
  })}\n\n`

describe('openRouteEventStream', () => {
  test('建连解析帧并回调 onLanes；带 Cookie 凭据与 SSE Accept 头', async () => {
    const stream = makeStreamBody()
    const fetchImpl = vi.fn(async () => ({
      ok: true,
      status: 200,
      body: stream.body,
    }))
    const onLanes = vi.fn()
    const statuses: string[] = []

    const handle = openRouteEventStream({
      onLanes,
      onStatus: (s) => statuses.push(s),
      fetchImpl: fetchImpl as unknown as typeof fetch,
      headers: async () => ({ Authorization: 'Bearer test' }),
    })

    stream.push(routeStateChunk([snapshot('model-1')]))
    await vi.waitFor(() => {
      expect(onLanes.mock.calls.length).toBeGreaterThan(0)
    })
    expect(onLanes.mock.calls[0]?.[0][0]?.lane).toBe('model-1')
    expect(statuses).toContain('open')

    const call = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(call[0]).toBe('/api/v1/route-events')
    expect(call[1].credentials).toBe('include')
    expect((call[1].headers as Record<string, string>).Accept).toBe(
      'text/event-stream'
    )
    expect((call[1].headers as Record<string, string>).Authorization).toBe(
      'Bearer test'
    )
    handle.close()
  })

  test('服务端断流后按注入的退避时长自动重连', async () => {
    const first = makeStreamBody()
    const second = makeStreamBody()
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, status: 200, body: first.body })
      .mockResolvedValueOnce({ ok: true, status: 200, body: second.body })
    const onLanes = vi.fn()
    const sleep = vi.fn(async () => {})
    const delayFor = vi.fn(() => 1234)

    const handle = openRouteEventStream({
      onLanes,
      fetchImpl: fetchImpl as unknown as typeof fetch,
      sleep,
      delayFor,
    })

    first.push(routeStateChunk([snapshot('model-1')]))
    await vi.waitFor(() => {
      expect(onLanes.mock.calls.length).toBeGreaterThan(0)
    })
    first.close() // 服务端断开

    await vi.waitFor(() => {
      expect(fetchImpl.mock.calls.length).toBeGreaterThanOrEqual(2)
    })
    expect(delayFor).toHaveBeenCalledWith(0)
    expect(sleep).toHaveBeenCalledWith(1234, expect.anything())

    second.push(routeStateChunk([snapshot('model-2')]))
    await vi.waitFor(() => {
      expect(onLanes.mock.calls.length).toBeGreaterThan(1)
    })
    expect(onLanes.mock.calls[1]?.[0][0]?.lane).toBe('model-2')
    handle.close()
  })

  test('close() 之后不再重连，并上报 closed 状态', async () => {
    const stream = makeStreamBody()
    const fetchImpl = vi.fn(async () => ({
      ok: true,
      status: 200,
      body: stream.body,
    }))
    const sleep = vi.fn(async () => {})
    const statuses: string[] = []

    const handle = openRouteEventStream({
      onLanes: vi.fn(),
      onStatus: (s) => statuses.push(s),
      fetchImpl: fetchImpl as unknown as typeof fetch,
      sleep,
      delayFor: () => 50,
    })
    await vi.waitFor(() => {
      expect(fetchImpl.mock.calls.length).toBeGreaterThan(0)
    })
    handle.close()
    stream.close()

    // close 后读循环结束（纯微任务链）；断言不再发起第二连、也不进入退避等待。
    await vi.waitFor(() => {
      expect(statuses.at(-1)).toBe('closed')
    })
    expect(fetchImpl.mock.calls.length).toBe(1)
    expect(sleep.mock.calls.length).toBe(0)
  })

  test('HTTP 非 2xx（如 401）走退避重连而不是崩溃', async () => {
    const okStream = makeStreamBody()
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce({ ok: false, status: 401, body: null })
      .mockResolvedValueOnce({ ok: true, status: 200, body: okStream.body })
    const onLanes = vi.fn()
    const delayFor = vi.fn(() => 100)

    const handle = openRouteEventStream({
      onLanes,
      fetchImpl: fetchImpl as unknown as typeof fetch,
      sleep: async () => {},
      delayFor,
    })
    await vi.waitFor(() => {
      expect(fetchImpl.mock.calls.length).toBeGreaterThanOrEqual(2)
    })
    expect(delayFor).toHaveBeenCalledWith(0)
    handle.close()
  })
})
