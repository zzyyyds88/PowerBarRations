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
import { useTranslation } from 'react-i18next'

import type { PBRSessionContextPoint } from '../../pbr/pbr-sessions-api'

function tone(ratio: number): string {
  if (ratio >= 0.9) return 'stroke-rose-500'
  if (ratio >= 0.75) return 'stroke-amber-500'
  return 'stroke-emerald-500'
}

/**
 * A session's context fill over its requests (usage-spec §7). The line is
 * scaled to the largest window seen; a session with no known window uses the
 * largest token count as the scale and shows absolute tokens in its title.
 */
export function ContextSparkline(props: {
  points: PBRSessionContextPoint[]
  truncated?: boolean
}) {
  const { t } = useTranslation()
  const points = props.points.filter((point) => point.tokens > 0)
  if (points.length === 0) {
    return (
      <p className='text-muted-foreground text-xs'>
        {t('No context was recorded for this session')}
      </p>
    )
  }
  const window = Math.max(...points.map((point) => point.window || 0))
  const peak = Math.max(...points.map((point) => point.tokens))
  const scale = window > 0 ? window : peak
  const width = 320
  const height = 64
  const step = points.length > 1 ? width / (points.length - 1) : width
  const line = points
    .map((point, index) => {
      const x = points.length > 1 ? index * step : width / 2
      const y = height - Math.min(1, point.tokens / scale) * (height - 4) - 2
      return `${x.toFixed(2)},${y.toFixed(2)}`
    })
    .join(' ')
  const last = points.at(-1)
  if (!last) return null
  const ratio = Math.min(1, last.tokens / scale)
  return (
    <div className='space-y-1'>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        className='text-muted-foreground h-16 w-full'
        role='img'
        aria-label={t('Context fill over the session')}
      >
        <line
          x1='0'
          x2={width}
          y1={height - 2}
          y2={height - 2}
          className='stroke-border'
          strokeWidth='1'
        />
        <polyline
          points={line}
          fill='none'
          className={tone(ratio)}
          strokeWidth='2'
          strokeLinejoin='round'
          strokeLinecap='round'
        />
        {points.map((point, index) => {
          const x = points.length > 1 ? index * step : width / 2
          const y =
            height - Math.min(1, point.tokens / scale) * (height - 4) - 2
          return (
            <circle
              key={`${point.id}-${point.ts}`}
              cx={x}
              cy={y}
              r='1.6'
              className={tone(Math.min(1, point.tokens / scale)).replace(
                'stroke-',
                'fill-'
              )}
            >
              <title>
                {`${new Date(point.ts).toLocaleString()} · ${point.tokens}${window > 0 ? ` / ${window} (${Math.round((point.tokens / window) * 100)}%)` : ''}`}
              </title>
            </circle>
          )
        })}
      </svg>
      <div className='text-muted-foreground flex justify-between text-xs'>
        <span>
          {t('Peak')} {peak.toLocaleString()}
        </span>
        {window > 0 && (
          <span>
            {t('Window')} {window.toLocaleString()}
          </span>
        )}
        {props.truncated && <span>{t('Showing the latest points')}</span>}
      </div>
    </div>
  )
}
