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
import { Button } from '@/components/ui/button'
import { searchChannels } from '@/features/channels/api'
import { channelsQueryKeys, getChannelTypeLabel } from '@/features/channels/lib'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  channelRouteKeys,
  findChannelPrice,
  matchesName,
} from '../lib/channel-price'

// 模型抽屉「渠道关联」：列出声明该模型（或经 model_mapping 映射它）的渠道，
// 展示每个渠道的上游单价（渠道 pbr_prices，单层单价：无渠道价即"未配置"），
// 并给出跳转到渠道编辑的入口。此处只读，仅用于成本展示。
const PAGE_SIZE = 100

function formatPrice(price: { input?: number; output?: number }): string {
  return `¥${price.input ?? 0} / ¥${price.output ?? 0}`
}

export function ModelLinkedChannels(props: {
  modelName: string
  nameRule?: number
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
      <div className='flex flex-col gap-1'>
        <h3 className='text-sm font-semibold'>{t('Channel association')}</h3>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Channel upstream price only; cost conversion only, never affects billing or admission.'
          )}
        </p>
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
            const channelPrice = findChannelPrice(
              channel,
              props.modelName,
              nameRule
            )
            let priceLabel = t('Not configured')
            if (channelPrice) {
              priceLabel = t('Channel price')
            }
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
                <div className='min-w-0 text-end'>
                  <p className='font-mono text-xs tabular-nums'>
                    {channelPrice
                      ? formatPrice(channelPrice)
                      : t('Not configured')}
                    {channelPrice ? (
                      <span className='text-muted-foreground'> · 1M</span>
                    ) : null}
                  </p>
                  <p className='text-muted-foreground text-xs'>{priceLabel}</p>
                </div>
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
