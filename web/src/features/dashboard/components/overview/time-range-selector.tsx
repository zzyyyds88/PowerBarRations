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
// 首页概览的时间分档选择器（ui-spec §6.2A）。
//
// 整页共用：选中档位换算成 from/to 后驱动下面所有卡片。选到「自定义区间」时
// 追加两个日期选择器（起止均含端）。

import { useTranslation } from 'react-i18next'

import { DatePicker } from '@/components/date-picker'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import {
  defaultCustomRange,
  PBR_TIME_RANGE_PRESETS,
  type PBRTimeRangePreset,
  type PBRTimeRangeSelection,
} from '../../lib/time-range'

export function TimeRangeSelector(props: {
  selection: PBRTimeRangeSelection
  onChange: (selection: PBRTimeRangeSelection) => void
  className?: string
}) {
  const { t } = useTranslation()

  // 字面量 t('...') 保证新增文案能被 i18n 覆盖扫描捕获。
  const presetLabels: Record<PBRTimeRangePreset, string> = {
    '24h': t('24 hours'),
    today: t('Today'),
    '3d': t('Last 3 days'),
    '7d': t('Last 7 days'),
    '30d': t('Last 30 days'),
    custom: t('Custom range'),
  }

  const handlePresetChange = (value: string) => {
    const preset = value as PBRTimeRangePreset
    if (preset === 'custom') {
      // 首次切到自定义时给「含今日的最近 7 天」作为默认起止。
      const fallback = defaultCustomRange()
      props.onChange({
        preset,
        start: props.selection.start ?? fallback.start,
        end: props.selection.end ?? fallback.end,
      })
      return
    }
    props.onChange({ ...props.selection, preset })
  }

  return (
    <div className={cn('flex flex-wrap items-center gap-2', props.className)}>
      <Tabs
        value={props.selection.preset}
        onValueChange={(value) => handlePresetChange(String(value))}
      >
        <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
          {PBR_TIME_RANGE_PRESETS.map((preset) => (
            <TabsTrigger key={preset} value={preset}>
              {presetLabels[preset]}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      {props.selection.preset === 'custom' && (
        <div className='flex flex-wrap items-center gap-2'>
          <DatePicker
            selected={props.selection.start}
            onSelect={(date) =>
              props.onChange({ ...props.selection, start: date })
            }
            placeholder={t('Start date')}
          />
          <DatePicker
            selected={props.selection.end}
            onSelect={(date) =>
              props.onChange({ ...props.selection, end: date })
            }
            placeholder={t('End date')}
          />
        </div>
      )}
    </div>
  )
}
