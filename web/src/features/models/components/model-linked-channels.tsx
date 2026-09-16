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
import {
  channelsQueryKeys,
  extractMappingSourceModels,
  getChannelTypeLabel,
  parseChannelSettings,
  parseModelsList,
} from '@/features/channels/lib'
import type { Channel, ChannelModelPrice } from '@/features/channels/types'
import { requireServerSuccess } from '@/lib/server-error-message'

import { usePBRModelPrices } from '../pbr-model-prices'

// 模型抽屉「渠道关联」：列出声明该模型（或经 model_mapping 映射它）的渠道，
// 展示每个渠道的上游单价（渠道 pbr_prices 优先，否则回退全局 PBRModelPrices），
// 并给出跳转到渠道编辑的入口。此处只读，仅用于成本展示。
const PAGE_SIZE = 100

function matchesName(name: string, modelName: string, rule: number): boolean {
  switch (rule) {
    case 1:
      return name.startsWith(modelName)
    case 2:
      return name.includes(modelName)
    case 3:
      return name.endsWith(modelName)
    default:
      return name === modelName
  }
}

function channelRouteKeys(channel: Channel): string[] {
  const keys = [
    ...parseModelsList(channel.models),
    ...extractMappingSourceModels(channel.model_mapping ?? ''),
  ]
  return [...new Set(keys)]
}

function findChannelPrice(
  channel: Channel,
  modelName: string,
  rule: number
): ChannelModelPrice | undefined {
  const prices = parseChannelSettings(channel.setting).pbr_prices ?? []
  if (rule === 0) return prices.find((item) => item.model === modelName)
  return prices.find((item) => matchesName(item.model, modelName, rule))
}

function formatPrice(price: { input?: number; output?: number }): string {
  return `¥${price.input ?? 0} / ¥${price.output ?? 0}`
}

export function ModelLinkedChannels(props: {
  modelName: string
  nameRule?: number
}) {
  const { t } = useTranslation()
  const nameRule = props.nameRule ?? 0
  const priceQuery = usePBRModelPrices()
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
  const globalPrice = priceQuery.data?.get(props.modelName)
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
            'Channel price takes precedence; cost conversion only, never affects billing or admission.'
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
            const effective = channelPrice ?? globalPrice
            let priceLabel = t('Not configured')
            if (channelPrice) {
              priceLabel = t('Channel price')
            } else if (globalPrice) {
              priceLabel = t('Global default')
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
                    {effective ? formatPrice(effective) : t('Not configured')}
                    {effective ? (
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
