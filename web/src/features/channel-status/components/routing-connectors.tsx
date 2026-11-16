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
import { useLayoutEffect, useState, type RefObject } from 'react'

interface Point {
  key: string
  x: number
  y: number
}

interface ConnectorGeometry {
  hubLeft: number
  hubRight: number
  hubY: number
  lanes: Point[]
  members: Point[]
}

export interface RoutingConnectorsProps {
  containerRef: RefObject<HTMLDivElement | null>
  hubRef: RefObject<HTMLDivElement | null>
  laneRefs: RefObject<Map<string, HTMLButtonElement>>
  memberRefs: RefObject<Map<string, HTMLDivElement>>
  laneKeys: string[]
  memberKeys: string[]
  selectedLane: string
  /** 正在服务的成员（线在流动）。 */
  currentMemberKey?: string
  /** 被绕开的成员（冷却/熔断/连拒）：线画成 rest。 */
  restingMemberKeys?: string[]
  /** 该车道此刻是否真的有流量：false 时没有任何线流动。 */
  laneBusy?: boolean
  laneAccent: (lane: string) => string
}

const bezier = (fromX: number, fromY: number, toX: number, toY: number) => {
  const mid = Math.max(28, Math.abs(toX - fromX) / 2)
  return `M ${fromX} ${fromY} C ${fromX + mid} ${fromY}, ${toX - mid} ${toY}, ${toX} ${toY}`
}

/**
 * 车道 → 网关 → 成员的虚线连线（magpie 同款视觉）。
 *
 * 位置由真实 DOM 测量，不按固定行高猜：成员数、语言、字号都会改变行高。
 * 只读装饰：`aria-hidden`、`pointer-events-none`；`lg` 以下布局变为单列，
 * 由 CSS 隐藏整层。
 */
export function RoutingConnectors(props: RoutingConnectorsProps) {
  const [geometry, setGeometry] = useState<ConnectorGeometry | null>(null)
  // ref 挂载完成后再触发一次 effect：ref 对象本身不会变，不能只靠它做依赖。
  const [mountTick, setMountTick] = useState(0)
  const laneKey = props.laneKeys.join('\n')
  const memberKey = props.memberKeys.join('\n')

  useLayoutEffect(() => {
    const container = props.containerRef.current
    const hub = props.hubRef.current
    if (!container || !hub) {
      // DOM 还没挂上（异步 chunk / 懒加载）：下一帧再来，最多等 20 帧。
      if (mountTick < 20) {
        const raf = requestAnimationFrame(() => setMountTick((n) => n + 1))
        return () => cancelAnimationFrame(raf)
      }
      return
    }
    const media =
      typeof window.matchMedia === 'function'
        ? window.matchMedia('(min-width: 1024px)')
        : null

    const laneNames = laneKey === '' ? [] : laneKey.split('\n')
    const memberNames = memberKey === '' ? [] : memberKey.split('\n')

    const measure = () => {
      if (media && !media.matches) {
        setGeometry(null)
        return
      }
      const containerRect = container.getBoundingClientRect()
      const hubRect = hub.getBoundingClientRect()
      if (containerRect.width === 0 || hubRect.width === 0) {
        setGeometry(null)
        return
      }
      const relative = (rect: DOMRect): { x: number; y: number } => ({
        x: rect.left - containerRect.left,
        y: rect.top - containerRect.top + rect.height / 2,
      })
      const hubPoint = relative(hubRect)
      const lanes: Point[] = []
      for (const key of laneNames) {
        const el = props.laneRefs.current?.get(key)
        if (!el) continue
        const rect = el.getBoundingClientRect()
        lanes.push({
          key,
          x: rect.right - containerRect.left,
          y: rect.top - containerRect.top + rect.height / 2,
        })
      }
      const members: Point[] = []
      for (const key of memberNames) {
        const el = props.memberRefs.current?.get(key)
        if (!el) continue
        const rect = el.getBoundingClientRect()
        members.push({
          key,
          x: rect.left - containerRect.left,
          y: rect.top - containerRect.top + rect.height / 2,
        })
      }
      setGeometry({
        hubLeft: hubPoint.x - hubRect.width / 2,
        hubRight: hubPoint.x + hubRect.width / 2,
        hubY: hubPoint.y,
        lanes,
        members,
      })
    }

    // 首次测量可能早于布局稳定（懒加载 chunk 挂载、字体未就绪、CSS 刚应用），
    // 此时宽度为 0 会直接放弃；而容器尺寸之后可能不再变化，ResizeObserver
    // 也就不会补一次。故先按帧重试到拿到非零尺寸为止（有界，最多 20 帧）。
    let raf = 0
    let tries = 0
    const settle = () => {
      measure()
      tries += 1
      const containerRect = container.getBoundingClientRect()
      const hubRect = hub.getBoundingClientRect()
      const ready =
        containerRect.width > 0 &&
        hubRect.width > 0 &&
        (!media || media.matches)
      if (!ready && tries < 20) raf = requestAnimationFrame(settle)
    }
    settle()

    const observer =
      typeof ResizeObserver === 'function' ? new ResizeObserver(measure) : null
    // 容器、网关与每个成员行都可能是尺寸变化的那一个：只观察容器会漏掉
    // 「容器高度不变但成员行由 0 变为有高度」的情况。
    observer?.observe(container)
    observer?.observe(hub)
    for (const el of props.laneRefs.current?.values() ?? []) {
      observer?.observe(el)
    }
    for (const el of props.memberRefs.current?.values() ?? []) {
      observer?.observe(el)
    }
    window.addEventListener('resize', measure)
    media?.addEventListener('change', measure)
    return () => {
      if (raf) cancelAnimationFrame(raf)
      observer?.disconnect()
      window.removeEventListener('resize', measure)
      media?.removeEventListener('change', measure)
    }
    // 依赖里故意不写 ref 对象：ref 的身份恒定，把它放进来不会让 effect 重跑；
    // 真正的重测信号是「结构 key」与「挂载计数」。生产启用 autoCodeSplitting 后
    // 连接器作为异步 chunk 挂载，首次 effect 可能面对空 DOM，故用 mountTick
    // 逐帧重试；成员行增减由 ResizeObserver 兜住。
    // eslint-disable-next-line react-hooks/exhaustive-deps -- refs 是稳定身份，见上
  }, [laneKey, memberKey, mountTick])

  if (!geometry) return null

  const resting = new Set(props.restingMemberKeys ?? [])
  const accent = props.laneAccent(props.selectedLane)
  const current = geometry.members.find(
    (member) => member.key === props.currentMemberKey
  )
  const busy = Boolean(props.laneBusy && current)

  /** 车道 → 网关：选中且车道在服务时流动，否则静止上色/压暗。 */
  const laneWireClass = (key: string): string => {
    if (key !== props.selectedLane) return 'pbr-wire pbr-wire-rest'
    return busy ? 'pbr-wire pbr-wire-live' : 'pbr-wire'
  }

  /**
   * 网关 → 成员：只有正在服务的成员流动（magpie 的 `live`）；被绕开的成员
   * 用 `rest`；其余静止。全部线都动会同时失去方向感和帧率。
   */
  const memberWireClass = (key: string): string => {
    if (key === props.currentMemberKey) {
      return busy ? 'pbr-wire pbr-wire-live' : 'pbr-wire'
    }
    if (resting.has(key)) return 'pbr-wire pbr-wire-rest'
    return 'pbr-wire'
  }

  /** 流动线上的数据包：沿二次贝塞尔中点放一个，不用 getPointAtLength 逐帧算。 */
  const packetAt = (fromX: number, fromY: number, toX: number, toY: number) => {
    const midX = (fromX + toX) / 2
    const midY = (fromY + toY) / 2
    return { x: midX, y: midY }
  }

  return (
    <svg
      aria-hidden='true'
      className='pointer-events-none absolute inset-0 z-0 hidden h-full w-full lg:block'
    >
      {geometry.lanes.map((lane) => (
        <path
          key={`lane-${lane.key}`}
          d={bezier(lane.x, lane.y, geometry.hubLeft, geometry.hubY)}
          className={laneWireClass(lane.key)}
          stroke={
            lane.key === props.selectedLane ? accent : 'var(--muted-foreground)'
          }
          strokeWidth={lane.key === props.selectedLane ? 1.5 : 1}
        />
      ))}
      {geometry.members.map((member) => (
        <path
          key={`member-${member.key}`}
          d={bezier(geometry.hubRight, geometry.hubY, member.x, member.y)}
          className={memberWireClass(member.key)}
          stroke={
            member.key === props.currentMemberKey
              ? accent
              : 'var(--muted-foreground)'
          }
          strokeWidth={member.key === props.currentMemberKey ? 1.5 : 1}
        />
      ))}
      {busy && current && (
        <circle
          className='pbr-wire-packet'
          r={3}
          {...packetAt(geometry.hubRight, geometry.hubY, current.x, current.y)}
        />
      )}
    </svg>
  )
}
