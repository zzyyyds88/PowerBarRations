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
渠道状态详情抽屉（ui-spec §6.4「状态」入口与详情抽屉；数据口径 api-spec §6.12）。

只读监控面：任何变更仍走渠道编辑器与既有动作，这里唯一的写动作是「立即探活」
（`POST /api/channels/{name}/test`，每次探活落一条探活记录）。数据一次取
`GET /api/channels/{name}/status?window=`，`24h` / `7d` 切换即按新 window 重新取数。
`now` 用查询的 `dataUpdatedAt` 注入（渲染期禁调 Date.now，lint react(purity)）。
*/
import { useQuery } from '@tanstack/react-query'
import { Activity } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  sideDrawerContentClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { ErrorState } from '@/components/error-state'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { channelsQueryKeys } from '../../lib'
import { formatDurationMs } from '../../lib/channel-status-display'
import {
  getChannelStatus,
  probeChannelStatus,
  type ChannelStatusDetail,
  type ChannelStatusWindow,
} from '../../status-api'
import type { Channel } from '../../types'
import { useChannels } from '../channels-provider'
import { ChannelStatusCurrentSection } from './channel-status-current'
import { ChannelStatusHistorySections } from './channel-status-history'
import { ChannelStatusModelsSection } from './channel-status-models'
import { ChannelStatusUpstreamSection } from './channel-status-upstream'
import { ChannelStatusWindowSection } from './channel-status-window'

export interface ChannelStatusDrawerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  channel: Channel
}

/** 加载态骨架：按六块内容各给一段占位，避免抽屉白屏。 */
function ChannelStatusDrawerSkeleton() {
  return (
    <div className='flex flex-col gap-6'>
      {['status', 'models', 'window', 'failures', 'probes', 'upstream'].map(
        (section) => (
          <div key={section} className='flex flex-col gap-3'>
            <Skeleton className='h-4 w-32' />
            <Skeleton className='h-20 w-full' />
          </div>
        )
      )}
    </div>
  )
}

function ChannelStatusDrawerContent(props: {
  detail: ChannelStatusDetail
  channel: Channel
  now: number
  timeWindow: ChannelStatusWindow
  onTimeWindowChange: (window: ChannelStatusWindow) => void
  isProbing: boolean
  isDetecting: boolean
  onProbe: () => void
  onDetect: () => void
}) {
  return (
    <>
      <ChannelStatusCurrentSection
        detail={props.detail}
        channel={props.channel}
        now={props.now}
      />
      <ChannelStatusModelsSection models={props.detail.models} />
      <ChannelStatusWindowSection
        summary={props.detail.summary}
        daily={props.detail.daily}
        timeWindow={props.timeWindow}
        onTimeWindowChange={props.onTimeWindowChange}
      />
      <ChannelStatusHistorySections
        failures={props.detail.recent_failures}
        probes={props.detail.probe_history}
        isProbing={props.isProbing}
        onProbe={props.onProbe}
      />
      <ChannelStatusUpstreamSection
        updates={props.detail.upstream_updates}
        isDetecting={props.isDetecting}
        onDetect={props.onDetect}
      />
    </>
  )
}

export function ChannelStatusDrawer(props: ChannelStatusDrawerProps) {
  const { t } = useTranslation()
  const { upstream } = useChannels()
  const [timeWindow, setTimeWindow] = useState<ChannelStatusWindow>('24h')
  const [isProbing, setIsProbing] = useState(false)
  const [isDetecting, setIsDetecting] = useState(false)

  const statusQuery = useQuery({
    queryKey: channelsQueryKeys.status(props.channel.name, timeWindow),
    queryFn: () => getChannelStatus(props.channel.name, timeWindow),
    enabled: props.open,
    // 失败态在抽屉内联展示（ErrorState + 重试），不叠加全局 toast。
    meta: { errorToast: false },
  })

  const handleProbe = async () => {
    setIsProbing(true)
    try {
      const result = await probeChannelStatus(props.channel.name)
      if (result.ok) {
        toast.success(t('Probe succeeded'), {
          description: t('Response time: {{duration}}', {
            duration: formatDurationMs(result.duration_ms),
          }),
        })
      } else {
        toast.error(t('Probe failed'), {
          description: result.error || result.msg || result.upstream_model,
        })
      }
    } catch (error) {
      handleServerError(error, t('Probe failed'))
    } finally {
      // 探活记录无论成败都落库，因此两条路径都要刷新历史与运行态。
      await statusQuery.refetch()
      setIsProbing(false)
    }
  }

  const handleDetect = async () => {
    setIsDetecting(true)
    try {
      // 既有上游变动流程（不新增动作）：结果写回渠道设置后，详情里的
      // 待处理清单才会变化，因此完成后重取一次。
      await upstream.detectChannelUpdates(props.channel)
    } finally {
      await statusQuery.refetch()
      setIsDetecting(false)
    }
  }

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent
        className={sideDrawerContentClassName('max-w-none sm:!max-w-[720px]')}
      >
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle className='flex items-center gap-2'>
            <Activity
              className='text-muted-foreground size-4'
              aria-hidden='true'
            />
            <span className='truncate'>{t('Channel status')}</span>
          </SheetTitle>
          <SheetDescription className='truncate'>
            {props.channel.name}
          </SheetDescription>
        </SheetHeader>

        <div className={sideDrawerFormClassName('gap-6')}>
          {statusQuery.isPending && <ChannelStatusDrawerSkeleton />}

          {statusQuery.isError && (
            <ErrorState
              title={t('Channel status is unavailable')}
              description={
                getServerErrorMessage(statusQuery.error) ||
                t('Failed to load channel status, please retry.')
              }
              onRetry={() => void statusQuery.refetch()}
              className='min-h-[240px]'
            />
          )}

          {statusQuery.data && (
            <ChannelStatusDrawerContent
              detail={statusQuery.data}
              channel={props.channel}
              now={statusQuery.dataUpdatedAt}
              timeWindow={timeWindow}
              onTimeWindowChange={setTimeWindow}
              isProbing={isProbing}
              isDetecting={isDetecting}
              onProbe={() => void handleProbe()}
              onDetect={() => void handleDetect()}
            />
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
