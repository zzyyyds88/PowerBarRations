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
车道运行态单元格（ui-spec §4 + §6.3「徽章不重复」，2026-10-25 重排）：
只显示**异常成员**的实时状态徽章（当前成员 / 冷却中 / 熔断 / 半开 / 探测占用 / 亲和）。
车道级健康徽章只在卡片头部出现（lane-card），本组件不再重复渲染；
「人工关闭」只在成员行标灰呈现（lane-card 成员列表），这里不再出第二个徽章。
数据来自 useLaneRuntime（SSE 主源、30s 轮询兜底）；时间判定纯逻辑在
lib/lane-runtime-display.ts，`now` 由数据层注入（渲染期禁调 Date.now）。
*/
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import type { LaneHealthSnapshot } from '@/lib/route-events'

import { laneMemberStates } from '../lib/lane-runtime-display'

export interface LaneRuntimeCellProps {
  snapshot?: LaneHealthSnapshot
  /** 数据层注入的"当前时刻"（毫秒）；避免渲染期调用 Date.now。 */
  now: number
}

export function LaneRuntimeCell(props: LaneRuntimeCellProps) {
  const { t } = useTranslation()
  const snapshot = props.snapshot
  const now = props.now

  // 状态文案保持 t() 字面量以便 i18n 扫描（web/AGENTS §3.1）。
  const stateLabel = (label: string): string => {
    if (label === 'Cooldown') return t('Cooldown')
    if (label === 'Circuit open') return t('Circuit open')
    if (label === 'Half-open') return t('Half-open')
    if (label === 'Affinity') return t('Affinity')
    if (label === 'Probing') return t('Probing')
    if (label === 'Manually disabled') return t('Manually disabled')
    return t('Current')
  }

  if (!snapshot) {
    return (
      <span className='text-muted-foreground text-xs'>
        {t('Waiting for runtime data')}
      </span>
    )
  }

  // 只显示异常成员的实时状态：正常成员（无任何徽章）不逐个罗列——车道级健康
  // 已由卡片头部徽章表达，这里逐个罗列正常态只是噪声（ui-spec §6.3 重排）。
  // 「人工关闭」是配置态，卡片成员行已标灰呈现，这里滤掉不再重复出徽章。
  const memberBadges = snapshot.members.flatMap((member) =>
    laneMemberStates(snapshot, member, now)
      .filter((state) => state.label !== 'Manually disabled')
      .map((state) => (
        <StatusBadge
          key={`${member.channel}/${member.upstream_model}/${state.label}`}
          label={`${member.channel} · ${stateLabel(state.label)}`}
          variant={state.variant}
          pulse={state.pulse}
          size='sm'
          copyable={false}
          title={
            state.detail
              ? t('until {{time}}', { time: state.detail })
              : undefined
          }
        />
      ))
  )

  if (memberBadges.length === 0) {
    return (
      <span className='text-muted-foreground text-xs'>{t('All stable')}</span>
    )
  }

  return (
    <div className='flex min-w-0 flex-wrap items-center gap-1'>
      {memberBadges}
    </div>
  )
}
