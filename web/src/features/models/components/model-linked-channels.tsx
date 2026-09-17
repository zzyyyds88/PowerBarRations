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
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { SideDrawerSection } from '@/components/drawer-layout'
import { ErrorState } from '@/components/error-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { searchChannels } from '@/features/channels/api'
import {
  CHANNEL_STATUS,
  CHANNEL_STATUS_CONFIG,
} from '@/features/channels/constants'
import { channelsQueryKeys, getChannelTypeLabel } from '@/features/channels/lib'
import { getLobeIcon } from '@/lib/lobe-icon'
import { requireServerSuccess } from '@/lib/server-error-message'

import { channelRouteKeys, matchesName } from '../lib/channel-price'
import { resolveModelIconKey } from '../lib/model-icon'

// 模型弹窗「渠道关联」（模型页 = 元数据页，ui-spec §6.3）：列出声明该模型
// （或经 model_mapping 映射它）的渠道，展示渠道名、类型与可用状态徽章，
// 并给出跳转到渠道编辑的入口。此处只读；计价只在渠道编辑「上游单价」页签，
// 不在此展示。
const PAGE_SIZE = 100

export function ModelLinkedChannels(props: {
  modelName: string
  nameRule?: number
  /** 模型元数据图标（与模型列表同源：显式 icon > 厂商推断 > 首字符）。 */
  icon?: string
}) {
  const { t } = useTranslation()
  const nameRule = props.nameRule ?? 0
  const channelsQuery = useQuery({
    queryKey: channelsQueryKeys.list({
      model: props.modelName,
      p: 1,
      page_size: PAGE_SIZE,
    }),
    queryFn: async () =>
      requireServerSuccess(
        await searchChannels({
          model: props.modelName,
          p: 1,
          page_size: PAGE_SIZE,
        })
      ),
    enabled: Boolean(props.modelName),
    staleTime: 60 * 1000,
  })
  const channels = useMemo(() => {
    const items = channelsQuery.data?.data?.items ?? []
    return items.filter((channel) =>
      channelRouteKeys(channel).some((name) =>
        matchesName(name, props.modelName, nameRule)
      )
    )
  }, [channelsQuery.data, props.modelName, nameRule])

  return (
    <SideDrawerSection>
      <div className='flex items-start gap-2.5'>
        <span className='mt-0.5 flex size-6 shrink-0 items-center justify-center'>
          {getLobeIcon(
            resolveModelIconKey({
              model_name: props.modelName,
              icon: props.icon,
            }),
            24
          )}
        </span>
        <div className='flex min-w-0 flex-col gap-1'>
          <h3 className='text-sm font-semibold'>{t('Channel association')}</h3>
          <p className='text-muted-foreground text-xs'>
            {t('Declarations and availability of this model across channels.')}
          </p>
        </div>
      </div>

      {channelsQuery.isError ? (
        <ErrorState
          description={channelsQuery.error.message}
          onRetry={() => void channelsQuery.refetch()}
        />
      ) : null}

      {!channelsQuery.isError && channelsQuery.isPending ? (
        <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
      ) : null}

      {!channelsQuery.isError &&
      !channelsQuery.isPending &&
      channels.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('No channel serves this model yet.')}
        </p>
      ) : null}

      {!channelsQuery.isError &&
      !channelsQuery.isPending &&
      channels.length > 0 ? (
        <ul className='flex flex-col gap-2'>
          {channels.map((channel) => {
            const statusConfig =
              CHANNEL_STATUS_CONFIG[
                channel.status as keyof typeof CHANNEL_STATUS_CONFIG
              ] ?? CHANNEL_STATUS_CONFIG[CHANNEL_STATUS.UNKNOWN]
            return (
              <li
                key={channel.id}
                className='flex items-center justify-between gap-3 rounded-lg border px-3 py-2'
              >
                <div className='min-w-0'>
                  <p className='truncate text-sm font-medium'>{channel.name}</p>
                  <p className='text-muted-foreground text-xs'>
                    {t(getChannelTypeLabel(channel.type))}
                  </p>
                </div>
                <StatusBadge
                  label={t(statusConfig.label)}
                  variant={statusConfig.variant}
                  size='sm'
                  copyable={false}
                />
                <Button
                  variant='outline'
                  size='sm'
                  render={
                    <Link to='/channels' search={{ filter: channel.name }} />
                  }
                >
                  {t('Edit channel')}
                </Button>
              </li>
            )
          })}
        </ul>
      ) : null}
    </SideDrawerSection>
  )
}
