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
import { useTranslation } from 'react-i18next'

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { searchChannels } from '@/features/channels/api'
import { channelsQueryKeys } from '@/features/channels/lib'
import type { ChannelModelPrice } from '@/features/channels/types'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  channelRouteKeys,
  findChannelPrice,
  matchesName,
} from '../lib/channel-price'

// 模型页「上游单价」：展示**渠道上游单价**（人民币/百万 token）。
//
// 单层单价（design-v1 §16.9#7）：价格只来自渠道级 pbr_prices。同一模型在多个
// 渠道可能有不同采购价，此时显示区间，悬停展开逐渠道明细；没有渠道价即
// "未配置"（不折算）。仅用于成本折算，不参与准入、不扣额度。
const ALL_CHANNELS_PAGE_SIZE = 100

type PricedChannel = { channel: string; price: ChannelModelPrice }

export function ModelUnitPriceCell(props: {
  modelName: string
  nameRule?: number
}) {
  const { t } = useTranslation()
  const nameRule = props.nameRule ?? 0
  // 同一 queryKey 在整张表里共享一次请求；渠道价是"逐渠道"的，故需要渠道清单。
  const channelsQuery = useQuery({
    queryKey: channelsQueryKeys.list({
      p: 1,
      page_size: ALL_CHANNELS_PAGE_SIZE,
    }),
    queryFn: async () =>
      requireServerSuccess(
        await searchChannels({ p: 1, page_size: ALL_CHANNELS_PAGE_SIZE })
      ),
    staleTime: 60 * 1000,
  })

  const channels = channelsQuery.data?.data?.items ?? []
  const pricedChannels: PricedChannel[] = channels
    .filter((channel) =>
      channelRouteKeys(channel).some((name) =>
        matchesName(name, props.modelName, nameRule)
      )
    )
    .map((channel) => ({
      channel: channel.name,
      price: findChannelPrice(channel, props.modelName, nameRule),
    }))
    .filter((item): item is PricedChannel => item.price !== undefined)

  if (pricedChannels.length === 0) {
    return (
      <span className='text-muted-foreground text-xs'>
        {t('Not configured')}
      </span>
    )
  }

  const inputs = pricedChannels.map((item) => item.price.input ?? 0)
  const outputs = pricedChannels.map((item) => item.price.output ?? 0)
  const minIn = Math.min(...inputs)
  const maxIn = Math.max(...inputs)
  const minOut = Math.min(...outputs)
  const maxOut = Math.max(...outputs)
  const inputText = minIn === maxIn ? `¥${minIn}` : `¥${minIn}–${maxIn}`
  const outputText = minOut === maxOut ? `¥${minOut}` : `¥${minOut}–${maxOut}`
  return (
    <Tooltip>
      <TooltipTrigger
        render={<span className='font-mono text-xs tabular-nums' />}
      >
        {`${inputText} / ${outputText}`}
        <span className='text-muted-foreground'> · 1M · </span>
        <span className='text-muted-foreground'>{t('Channel price')}</span>
      </TooltipTrigger>
      <TooltipContent role='tooltip'>
        <div className='space-y-0.5 text-xs'>
          {pricedChannels.map((item) => (
            <div key={item.channel}>
              {`${item.channel}: ¥${item.price.input ?? 0} / ¥${item.price.output ?? 0}`}
            </div>
          ))}
        </div>
      </TooltipContent>
    </Tooltip>
  )
}
