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
抽屉第 2 块「窗口概览」（ui-spec §6.4-2）：请求 / 成功 / 失败 / 成功率 / 平均耗时 /
平均首字耗时 / 折算成本，附逐日请求与失败的小柱状图。`24h` / `7d` 切换即按新
window 重新取数（切换控件在本节标题行，状态由抽屉持有）。

逐日图只用 `daily` 给出的日粒度（`24h` 最多 2 个桶、`7d` 最多 7 个），
**不做小时插值**；`summary.avg_total_ms` / `avg_ttft_ms` 来自明细日志
（`POST /logs/prune` 后会回落为 0），与聚合表口径的请求数/成功率区分展示。
*/
import { BarChart3 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatCostYuan } from '@/features/keys/lib/cost'
import { formatNumber } from '@/lib/format'

import {
  formatDailyLabel,
  formatDurationMs,
  formatSuccessRate,
} from '../../lib/channel-status-display'
import type {
  ChannelStatusDailyPoint,
  ChannelStatusSummary,
  ChannelStatusWindow,
} from '../../status-api'

/** 指标小卡：与看板的数字卡同一视觉语言（bg-muted/40 圆角块 + 大号数字）。 */
function StatTile(props: { label: string; value: string }) {
  return (
    <div className='bg-muted/40 rounded-lg border border-transparent px-3 py-2'>
      <div className='text-muted-foreground truncate text-[11px] leading-none font-medium'>
        {props.label}
      </div>
      <div
        className='mt-1.5 truncate text-sm font-semibold tabular-nums'
        title={props.value}
      >
        {props.value}
      </div>
    </div>
  )
}

/**
 * 逐日请求与失败柱状图：每根柱是当天全部请求，柱内红段是其中的失败。
 * 只有 `daily` 的日粒度，没有小时数据可画。
 */
function DailyBars(props: { daily: ChannelStatusDailyPoint[] }) {
  const { t } = useTranslation()

  if (props.daily.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('No requests in this window')}
      </p>
    )
  }

  const maxRequests = Math.max(
    1,
    ...props.daily.map((point) => Math.max(0, point.requests))
  )

  return (
    <div className='flex flex-col gap-2'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <h4 className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
          {t('Daily requests and failures')}
        </h4>
        <div className='text-muted-foreground flex items-center gap-3 text-[11px]'>
          <span className='flex items-center gap-1'>
            <span
              className='bg-primary/30 size-2 rounded-sm'
              aria-hidden='true'
            />
            {t('Requests')}
          </span>
          <span className='flex items-center gap-1'>
            <span
              className='bg-destructive size-2 rounded-sm'
              aria-hidden='true'
            />
            {t('Failures')}
          </span>
        </div>
      </div>

      <div
        role='img'
        aria-label={t('Daily requests and failures')}
        className='flex flex-col gap-1'
      >
        <div className='flex h-24 items-end gap-2'>
          {props.daily.map((point) => {
            const requests = Math.max(0, point.requests)
            const failures = Math.min(Math.max(0, point.failures), requests)
            const failureRatio = requests > 0 ? failures / requests : 0
            // 上限 78% 给柱顶的数字留位置（容器 h-24 是固定高度，百分比可解析）。
            const heightRatio =
              requests > 0 ? Math.max(8, (requests / maxRequests) * 78) : 4
            return (
              <div
                key={point.date}
                className='flex h-full min-w-0 flex-1 flex-col items-center justify-end gap-1'
              >
                <span className='text-muted-foreground text-[10px] tabular-nums'>
                  {requests}
                </span>
                <div
                  className='bg-primary/30 relative w-4 rounded-t-sm'
                  style={{ height: `${heightRatio}%` }}
                  title={`${point.date} · ${t('Requests')} ${requests} · ${t('Failures')} ${failures}`}
                >
                  {failures > 0 && (
                    <div
                      className='bg-destructive absolute inset-x-0 bottom-0 rounded-t-sm'
                      style={{ height: `${failureRatio * 100}%` }}
                    />
                  )}
                </div>
              </div>
            )
          })}
        </div>
        <div className='flex gap-2'>
          {props.daily.map((point) => (
            <span
              key={point.date}
              className='text-muted-foreground min-w-0 flex-1 text-center text-[10px] whitespace-nowrap'
            >
              {formatDailyLabel(point.date)}
            </span>
          ))}
        </div>
      </div>
    </div>
  )
}

export function ChannelStatusWindowSection(props: {
  summary: ChannelStatusSummary
  daily: ChannelStatusDailyPoint[]
  timeWindow: ChannelStatusWindow
  onTimeWindowChange: (window: ChannelStatusWindow) => void
}) {
  const { t } = useTranslation()
  const summary = props.summary

  return (
    <SideDrawerSection>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <SideDrawerSectionHeader
          title={t('Window overview')}
          icon={<BarChart3 className='size-4' />}
          iconTone='chart-2'
          className='min-w-0 flex-1'
        />
        <Tabs
          value={props.timeWindow}
          onValueChange={(value) =>
            props.onTimeWindowChange(value === '7d' ? '7d' : '24h')
          }
        >
          <TabsList>
            <TabsTrigger value='24h'>24h</TabsTrigger>
            <TabsTrigger value='7d'>7d</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
        <StatTile
          label={t('Requests')}
          value={formatNumber(summary.requests)}
        />
        <StatTile
          label={t('Successes')}
          value={formatNumber(summary.successes)}
        />
        <StatTile
          label={t('Failures')}
          value={formatNumber(summary.failures)}
        />
        <StatTile
          label={t('Success rate')}
          value={formatSuccessRate(summary.success_rate)}
        />
        <StatTile
          label={t('Average duration')}
          value={formatDurationMs(summary.avg_total_ms)}
        />
        <StatTile
          label={t('Average first token latency')}
          value={formatDurationMs(summary.avg_ttft_ms)}
        />
        <StatTile
          label={t('Cost (converted)')}
          value={formatCostYuan(summary.estimated_cost)}
        />
      </div>

      <DailyBars daily={props.daily} />
    </SideDrawerSection>
  )
}
