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
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { LOGS_TAIL_INTERVALS, type LogsTailIntervalMs } from '../constants'
import { useUsageLogsContext } from './usage-logs-provider'

function tailOptionLabel(t: TFunction, interval: LogsTailIntervalMs): string {
  if (interval === 0) return t('Off')
  return t('Every {{seconds}}s', { seconds: interval / 1000 })
}

/**
 * Live-tail cadence selector (ui-spec §6 日志): Off / 2s / 5s / 10s, default
 * Off. The value lives in `UsageLogsProvider`; the table turns it into a
 * `refetchInterval` and pauses it while the tab is hidden.
 */
export function RequestLogsTailSelect() {
  const { t } = useTranslation()
  const { tailIntervalMs, setTailIntervalMs } = useUsageLogsContext()
  const label = tailOptionLabel(t, tailIntervalMs)

  return (
    <Select
      value={String(tailIntervalMs)}
      onValueChange={(value) => {
        const interval = Number(value)
        if (LOGS_TAIL_INTERVALS.includes(interval as LogsTailIntervalMs)) {
          setTailIntervalMs(interval as LogsTailIntervalMs)
        }
      }}
    >
      <SelectTrigger className='h-8 w-40' aria-label={t('Live tail')}>
        {/* Explicit child: Base UI otherwise echoes the raw millisecond value. */}
        <SelectValue>{`${t('Live tail')}: ${label}`}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        {LOGS_TAIL_INTERVALS.map((interval) => (
          <SelectItem key={interval} value={String(interval)}>
            {tailOptionLabel(t, interval)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
