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
抽屉第 5 块「上游模型变动提示」（ui-spec §6.4-5）：上次检测时间与待处理的新增/移除
清单——数据来自渠道设置里上次"检测上游模型变动"的快照，本端点不访问上游、无副作用
（api-spec §6.12）。从未检测过时明示"从未检测"；两项清单都空时明示"无变动"。
「去检测」按钮走既有上游变动流程（不新增动作），因此这里只回调、不直接发请求。
*/
import { RefreshCw, RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'

import { formatStatusTime } from '../../lib/channel-status-display'
import type { ChannelStatusUpstreamUpdates } from '../../status-api'

/** 待处理模型清单：新增用绿色 `+`、移除用红色 `-`，与列表页的标记一致。 */
function PendingModels(props: {
  title: string
  models: string[]
  tone: 'add' | 'remove'
}) {
  if (props.models.length === 0) return null

  return (
    <div className='flex flex-col gap-1.5'>
      <div className='text-muted-foreground text-xs font-medium'>
        {props.title}
      </div>
      <ul className='flex flex-wrap gap-1.5'>
        {props.models.map((model) => (
          <li
            key={model}
            className={
              props.tone === 'add'
                ? 'bg-success/10 text-success rounded-md px-2 py-0.5 font-mono text-xs'
                : 'bg-destructive/10 text-destructive rounded-md px-2 py-0.5 font-mono text-xs'
            }
          >
            {props.tone === 'add' ? '+' : '-'}
            {model}
          </li>
        ))}
      </ul>
    </div>
  )
}

export function ChannelStatusUpstreamSection(props: {
  updates: ChannelStatusUpstreamUpdates
  isDetecting: boolean
  onDetect: () => void
}) {
  const { t } = useTranslation()
  const updates = props.updates
  const lastCheck = formatStatusTime(updates.last_check_ts)
  const hasPending =
    updates.pending_add.length > 0 || updates.pending_remove.length > 0

  return (
    <SideDrawerSection>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <SideDrawerSectionHeader
          title={t('Upstream model changes')}
          icon={<RefreshCw className='size-4' />}
          iconTone='chart-4'
          className='min-w-0 flex-1'
        />
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={props.onDetect}
          disabled={props.isDetecting}
        >
          {props.isDetecting ? (
            <RotateCw className='size-3.5 animate-spin' />
          ) : (
            <RefreshCw className='size-3.5' />
          )}
          {t('Detect now')}
        </Button>
      </div>

      <div className='flex flex-col gap-2 text-xs'>
        <div className='flex items-center gap-1.5'>
          <span className='text-muted-foreground'>{t('Last checked')}:</span>
          <span className='tabular-nums'>
            {lastCheck || t('Never checked')}
          </span>
        </div>

        {!hasPending && (
          <p className='text-muted-foreground text-sm'>
            {t('No upstream model changes')}
          </p>
        )}

        <PendingModels
          title={t('Pending add')}
          models={updates.pending_add}
          tone='add'
        />
        <PendingModels
          title={t('Pending remove')}
          models={updates.pending_remove}
          tone='remove'
        />
      </div>
    </SideDrawerSection>
  )
}
