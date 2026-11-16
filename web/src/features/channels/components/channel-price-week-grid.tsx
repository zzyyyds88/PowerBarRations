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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  buildWeekCoverage,
  formatClockHHMM,
  type PriceBand,
} from '../lib/channel-prices'

// 峰谷周视图（ui-spec §9）：7 天 × 24 小时。
//
// 每格显示覆盖该小时的第一条 band 序号（先命中者生效）；空格 = 该小时没有
// band 覆盖，用行级兜底价。跨午夜 band 会点亮次日凌晨的格子，但归属仍是
// 起始日（与后端匹配口径一致）。

const WEEK_DAY_KEYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'] as const

type ChannelPriceWeekGridProps = {
  bands: PriceBand[]
}

export function ChannelPriceWeekGrid(props: ChannelPriceWeekGridProps) {
  const { t } = useTranslation()
  const cells = useMemo(() => buildWeekCoverage(props.bands), [props.bands])

  return (
    <div className='overflow-x-auto'>
      <table
        aria-label={t('Weekly coverage')}
        className='min-w-[520px] border-separate border-spacing-0 text-[11px]'
      >
        <thead>
          <tr>
            <th
              scope='col'
              className='text-muted-foreground w-12 pb-1 text-left font-medium'
            >
              {t('Hours')}
            </th>
            {WEEK_DAY_KEYS.map((key) => (
              <th
                key={key}
                scope='col'
                className='text-muted-foreground pb-1 text-center font-medium'
              >
                {t(key)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {Array.from({ length: 24 }, (_, hour) => hour).map((hour) => (
            <tr key={hour}>
              <th
                scope='row'
                className='text-muted-foreground pr-1 text-right font-normal tabular-nums'
              >
                {formatClockHHMM(hour * 60)}
              </th>
              {WEEK_DAY_KEYS.map((key, dayIndex) => {
                const cell = cells.find(
                  (item) => item.day === dayIndex + 1 && item.hour === hour
                )
                const bandIndex = cell?.bandIndex ?? -1
                const covered = bandIndex >= 0
                return (
                  <td
                    key={key}
                    className={
                      covered
                        ? 'border-border bg-primary/25 text-foreground h-5 border text-center tabular-nums'
                        : 'border-border bg-muted/30 h-5 border text-center'
                    }
                    title={
                      covered
                        ? `${t(key)} ${formatClockHHMM(hour * 60)} · ${t('Band')} ${bandIndex + 1}`
                        : `${t(key)} ${formatClockHHMM(hour * 60)} · ${t('Row fallback (all day)')}`
                    }
                  >
                    {covered ? bandIndex + 1 : ''}
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
