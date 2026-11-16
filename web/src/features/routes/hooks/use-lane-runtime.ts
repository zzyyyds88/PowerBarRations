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
车道运行态数据源（ui-spec §4、routing-spec §7）：

- 主源：SSE `GET /api/route-events`（fetch + ReadableStream，断线自动重连，
  组件卸载时关闭连接）；
- 兜底：30s 轮询 `GET /api/v1/lanes/{name}/health`；
- SSE 新鲜时轮询结果只做对账（补 SSE 缺失的车道），不主导渲染，避免双源闪烁；
  合并规则是纯函数 resolveLaneSnapshots（lib 单测覆盖）。
*/
import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState } from 'react'

import { getFreshAuthHeaders } from '@/lib/api'
import {
  openRouteEventStream,
  resolveLaneSnapshots,
  type LaneHealthSnapshot,
  type ResolvedLaneRuntime,
} from '@/lib/route-events'

import { pollPBRLaneHealth } from '../api'

/** 轮询兜底节奏（routing-spec §7：对齐线上 refetchInterval 口径）。 */
export const LANE_HEALTH_POLL_INTERVAL_MS = 30_000

export const laneHealthQueryKey = ['pbr-lane-health'] as const

export interface LaneRuntimeState {
  /** 车道名 → 生效运行态快照（SSE 主导或对账补齐）。 */
  runtime: ResolvedLaneRuntime
  /** SSE 当前是否连着（展示层可用于"实时/兜底"提示）。 */
  sseConnected: boolean
  /**
   * 渲染层可用的"当前时刻"（毫秒）。渲染期禁调 Date.now（lint react(purity)），
   * 这里取两个数据事件（最后一帧 SSE / 最近一次轮询返回）的较新者：
   * SSE 每秒推帧时它逼近真实时间；SSE 失联时轮询（30s）会把它推过新鲜窗口，
   * 对账逻辑随即切换到轮询主导。
   */
  nowMs: number
}

/**
 * 订阅给定车道集合的运行态。
 *
 * @param laneNames 需要运行态的车道名（路由页：全部 explicit 车道）。
 */
export function useLaneRuntime(laneNames: string[]): LaneRuntimeState {
  const [sseLanes, setSseLanes] = useState<LaneHealthSnapshot[] | null>(null)
  const [sseReceivedAt, setSseReceivedAt] = useState<number | null>(null)
  const [sseConnected, setSseConnected] = useState(false)
  const lastPayloadRef = useRef<string | null>(null)

  useEffect(() => {
    const stream = openRouteEventStream({
      onLanes: (lanes) => {
        // 服务端每秒推一帧但多数内容与上一帧相同：内容不变时只刷新新鲜度，
        // 不产生新的 state 引用，避免无意义的整表重渲染。
        const payload = JSON.stringify(lanes)
        if (payload === lastPayloadRef.current) {
          setSseReceivedAt(Date.now())
          return
        }
        lastPayloadRef.current = payload
        setSseLanes(lanes)
        setSseReceivedAt(Date.now())
      },
      onStatus: (status) => setSseConnected(status === 'open'),
      headers: getFreshAuthHeaders,
    })
    return () => {
      stream.close()
    }
  }, [])

  // 车道名集合变化会让轮询参数变化；用稳定字符串做 queryKey 与查询入参，
  // 避免父组件每次 render 新建数组导致轮询查询反复重建。
  const namesKey = useMemo(() => [...laneNames].sort().join('\n'), [laneNames])

  const healthQuery = useQuery({
    queryKey: [...laneHealthQueryKey, namesKey],
    queryFn: () =>
      pollPBRLaneHealth(namesKey === '' ? [] : namesKey.split('\n')),
    refetchInterval: LANE_HEALTH_POLL_INTERVAL_MS,
    // 兜底源：失败不弹 toast（SSE 还在主渲染），也不做请求级重试。
    meta: { errorToast: false },
    retry: false,
  })

  const nowMs = Math.max(sseReceivedAt ?? 0, healthQuery.dataUpdatedAt ?? 0)

  const runtime = useMemo(
    () =>
      resolveLaneSnapshots({
        sseLanes,
        sseReceivedAt,
        sseConnected,
        pollLanes: healthQuery.data ?? null,
        now: nowMs,
      }),
    [sseLanes, sseReceivedAt, sseConnected, healthQuery.data, nowMs]
  )

  return { runtime, sseConnected, nowMs }
}
