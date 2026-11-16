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
import { ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { PBRLaneSummary } from '@/features/routes/api'
import type { LaneHealthSnapshot } from '@/lib/route-events'
import { cn } from '@/lib/utils'

import { laneAvailability } from '../lib/live-routing'

function availabilityTone(
  health: ReturnType<typeof laneAvailability>['health']
): string {
  if (health === 'healthy') return 'bg-success'
  if (health === 'degraded') return 'bg-warning'
  if (health === 'unavailable') return 'bg-destructive'
  return 'bg-muted-foreground/40'
}

export interface LaneRailProps {
  lanes: PBRLaneSummary[]
  selectedLane: string
  snapshots: ReadonlyMap<string, LaneHealthSnapshot>
  /** 车道名 → SVG/圆点强调色（按车道稳定分配）。 */
  accents: ReadonlyMap<string, string>
  onSelect: (lane: string) => void
  /** 连线锚点注册（父组件持有 ref Map）。 */
  registerRef: (lane: string, el: HTMLButtonElement | null) => void
}

/**
 * 实时路由左栏：一条车道一张卡，展示运行态可用成员数与模式。
 *
 * 移动端横向滚动（每张卡固定宽度），桌面端纵向排列；选中态用强调色左边条，
 * 与中间的连线颜色一致，用户不需要读图例就知道线属于哪条车道。
 */
export function LaneRail(props: LaneRailProps) {
  const { t } = useTranslation()

  return (
    <div
      className='flex gap-2 overflow-x-auto pb-1 lg:flex-col lg:overflow-visible lg:pb-0'
      role='list'
      aria-label={t('Lanes')}
    >
      {props.lanes.map((lane) => {
        const selected = lane.name === props.selectedLane
        const accent = props.accents.get(lane.name) ?? 'var(--chart-1)'
        const availability = laneAvailability(
          lane,
          props.snapshots.get(lane.name)
        )
        const pct =
          availability.total > 0
            ? Math.round((availability.available / availability.total) * 100)
            : 0
        const off = lane.enabled === false
        const mode = lane.mode === 'manual' ? t('Manual') : t('Failover')

        return (
          <div
            key={lane.name}
            role='listitem'
            className='w-[196px] shrink-0 lg:w-auto'
          >
            <button
              ref={(el) => props.registerRef(lane.name, el)}
              type='button'
              aria-pressed={selected}
              aria-label={`${lane.name} · ${mode} · ${availability.available}/${availability.total}`}
              onClick={() => props.onSelect(lane.name)}
              className={cn(
                'group w-full rounded-lg border px-3 py-2 text-left transition-colors',
                selected
                  ? 'border-foreground/25 bg-card shadow-sm'
                  : 'border-border/60 bg-muted/20 hover:border-border hover:bg-muted/40',
                off && 'opacity-60'
              )}
              style={
                selected
                  ? { boxShadow: `inset 2px 0 0 0 ${accent}` }
                  : undefined
              }
            >
              <span className='flex min-w-0 items-center gap-2'>
                <span
                  aria-hidden='true'
                  className='size-2.5 shrink-0 rounded-full'
                  style={{ backgroundColor: accent }}
                />
                <span className='min-w-0 flex-1 truncate text-sm font-medium'>
                  {lane.name}
                </span>
                <ChevronRight
                  aria-hidden='true'
                  className={cn(
                    'size-3.5 shrink-0 transition-transform',
                    selected
                      ? 'text-foreground'
                      : 'text-muted-foreground/50 group-hover:translate-x-0.5'
                  )}
                />
              </span>
              <span className='text-muted-foreground mt-0.5 flex items-center justify-between gap-2 text-[11px]'>
                <span className='tabular-nums'>
                  {availability.available}/{availability.total} {t('Available')}
                </span>
                {/* 故障切换是默认模式，逐卡重复只会变成噪声；手动模式才标出来。 */}
                {lane.mode === 'manual' && (
                  <span className='truncate'>{mode}</span>
                )}
              </span>
              <span className='bg-muted mt-1.5 block h-1 overflow-hidden rounded-full'>
                <span
                  aria-hidden='true'
                  className={cn(
                    'block h-full rounded-full transition-[width]',
                    availabilityTone(availability.health)
                  )}
                  style={{ width: `${pct}%` }}
                />
              </span>
              {off && (
                <span className='text-muted-foreground mt-1.5 block text-[11px]'>
                  {t('Lane disabled')}
                </span>
              )}
            </button>
          </div>
        )
      })}
    </div>
  )
}
