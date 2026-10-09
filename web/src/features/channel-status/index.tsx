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
渠道状态 `/channel-status`（ui-spec §6.4A）：侧边栏独立页，主体是"一眼看全"的
状态表——所有渠道现在怎么样（数据源 `GET /api/channels/status?window=`，
`24h` / `7d` 可切换）。点渠道名就地展开该渠道**每个上游模型**的成功率与输出速度
（`GET /api/channels/{name}/status` 的 `models[]`，api-spec §6.12），
行内「详情」打开与渠道列表同一个状态抽屉（`ChannelStatusDrawer`）。

协议列与抽屉协议徽章共用 `resolveProtocolLabelKey`：控制台渠道记录优先、
契约适配器名兜底。控制台记录同时给抽屉提供探测入口所需的 id，因此只在记录缺失
（渠道数超过一页）时才按名字补查一次，不静默失败。
*/
import { useQuery } from '@tanstack/react-query'
import { ChevronDown, ChevronRight, Loader2 } from 'lucide-react'
import { Fragment, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { getChannels, searchChannels } from '@/features/channels/api'
import { ChannelsProvider } from '@/features/channels/components/channels-provider'
import { ChannelStatusDrawer } from '@/features/channels/components/drawers/channel-status-drawer'
import { ChannelStatusModelsTable } from '@/features/channels/components/drawers/channel-status-models'
import { channelsQueryKeys } from '@/features/channels/lib'
import {
  formatStatusTime,
  formatSuccessRate,
  hasUpstreamReject,
  resolveProtocolLabelKey,
} from '@/features/channels/lib/channel-status-display'
import {
  getChannelStatus,
  listChannelStatus,
  type ChannelStatusSummaryItem,
  type ChannelStatusWindow,
} from '@/features/channels/status-api'
import type { Channel } from '@/features/channels/types'
import { formatCostYuan } from '@/features/keys/lib/cost'
import { formatNumber } from '@/lib/format'
import { getServerErrorMessage } from '@/lib/server-error-message'

/** 控制台渠道列表一次取回量（服务端上限即 100），用于协议与抽屉记录。 */
const CONSOLE_CHANNEL_PAGE_SIZE = 100

const HEAD_CLASS_NAME = 'whitespace-nowrap'

const statusListQueryKey = (window: ChannelStatusWindow) =>
  [...channelsQueryKeys.all, 'status-list', window] as const
const consoleChannelsQueryKey = () =>
  [...channelsQueryKeys.all, 'status-console-channels'] as const
const channelNameLookupQueryKey = (name: string) =>
  [...channelsQueryKeys.all, 'status-name-lookup', name] as const

function formatMembers(item: ChannelStatusSummaryItem): string {
  return `${formatNumber(item.available_count)}/${formatNumber(item.member_count)}`
}

/** 成功率无样本（窗口内没有请求）时显示 `-`，不拿 0% 冒充"全失败"。 */
function formatItemSuccessRate(item: ChannelStatusSummaryItem): string {
  if (!Number.isFinite(item.requests) || item.requests <= 0) return '-'
  return formatSuccessRate(item.success_rate)
}

function formatLastProbe(
  probe: ChannelStatusSummaryItem['last_probe']
): string {
  if (!probe) return '-'
  return formatStatusTime(probe.ts) || '-'
}

/** 行内展开的模型状态：按当前 window 取该渠道详情，复用抽屉里的模型表。 */
function ChannelStatusRowModels(props: {
  channel: string
  timeWindow: ChannelStatusWindow
}) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: channelsQueryKeys.status(props.channel, props.timeWindow),
    queryFn: () => getChannelStatus(props.channel, props.timeWindow),
    staleTime: 30 * 1000,
    meta: { errorToast: false },
  })

  if (query.isPending) {
    return (
      <div className='flex flex-col gap-2'>
        <Skeleton className='h-4 w-40' />
        <Skeleton className='h-16 w-full' />
      </div>
    )
  }

  if (query.isError) {
    return (
      <ErrorState
        title={t('Channel status is unavailable')}
        description={
          getServerErrorMessage(query.error) ||
          t('Failed to load channel status, please retry.')
        }
        onRetry={() => void query.refetch()}
        className='min-h-[160px]'
      />
    )
  }

  return (
    <div className='flex flex-col gap-2'>
      <h4 className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
        {t('Model status')}
      </h4>
      <ChannelStatusModelsTable models={query.data?.models ?? []} />
    </div>
  )
}

export function ChannelStatus() {
  const { t } = useTranslation()
  const [timeWindow, setTimeWindow] = useState<ChannelStatusWindow>('24h')
  const [expandedChannel, setExpandedChannel] = useState<string | null>(null)
  const [detailName, setDetailName] = useState<string | null>(null)

  const statusQuery = useQuery({
    queryKey: statusListQueryKey(timeWindow),
    queryFn: () => listChannelStatus(timeWindow),
    meta: { errorToast: false },
  })

  const consoleChannelsQuery = useQuery({
    queryKey: consoleChannelsQueryKey(),
    queryFn: () => getChannels({ p: 1, page_size: CONSOLE_CHANNEL_PAGE_SIZE }),
    staleTime: 60 * 1000,
    meta: { errorToast: false },
  })

  const consoleChannelsByName = useMemo(() => {
    const map = new Map<string, Channel>()
    for (const channel of consoleChannelsQuery.data?.items ?? []) {
      map.set(channel.name, channel)
    }
    return map
  }, [consoleChannelsQuery.data])

  // 抽屉里的「去检测」走既有上游变动流程（按 id），需要控制台渠道记录。
  const needsNameLookup =
    detailName !== null && !consoleChannelsByName.has(detailName)
  const nameLookupQuery = useQuery({
    queryKey: channelNameLookupQueryKey(detailName ?? ''),
    queryFn: () =>
      searchChannels({ keyword: detailName ?? '', p: 1, page_size: 20 }),
    enabled: needsNameLookup,
    staleTime: 60 * 1000,
    meta: { errorToast: false },
  })

  const detailChannel = useMemo(() => {
    if (!detailName) return null
    const fromConsoleList = consoleChannelsByName.get(detailName)
    if (fromConsoleList) return fromConsoleList
    return (
      (nameLookupQuery.data?.items ?? []).find(
        (channel) => channel.name === detailName
      ) ?? null
    )
  }, [consoleChannelsByName, detailName, nameLookupQuery.data])

  useEffect(() => {
    if (!needsNameLookup || nameLookupQuery.isPending) return
    if (detailChannel) return
    // 拿不到控制台记录就打不开抽屉（探测入口需要 id）——明说，不静默失败。
    toast.error(t('Channel not found'))
    setDetailName(null)
  }, [detailChannel, nameLookupQuery.isPending, needsNameLookup, t])

  const items = statusQuery.data?.items ?? []
  const upstreamRejected = hasUpstreamReject(
    items.map((item) => item.last_error_kind)
  )

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          <span className='truncate'>{t('Channel status')}</span>
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Tabs
            value={timeWindow}
            onValueChange={(value) =>
              setTimeWindow(value === '7d' ? '7d' : '24h')
            }
          >
            <TabsList>
              <TabsTrigger value='24h'>24h</TabsTrigger>
              <TabsTrigger value='7d'>7d</TabsTrigger>
            </TabsList>
          </Tabs>
        </SectionPageLayout.Actions>

        <SectionPageLayout.Content>
          {statusQuery.isPending && (
            <div className='flex flex-col gap-3'>
              {['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'].map((row) => (
                <Skeleton key={row} className='h-10 w-full' />
              ))}
            </div>
          )}

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

          {statusQuery.data && items.length === 0 && (
            <EmptyState
              title={t('No channels yet')}
              description={t(
                'No channels available. Create your first channel to get started.'
              )}
            />
          )}

          {statusQuery.data && items.length > 0 && (
            <div className='flex flex-col gap-3'>
              {upstreamRejected && (
                <p className='text-muted-foreground bg-muted/40 rounded-lg px-3 py-2 text-xs'>
                  {t('Upstream rejected the request (not a channel fault).')}
                </p>
              )}

              <Table className='text-xs'>
                <TableHeader>
                  <TableRow>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Channel')}
                    </TableHead>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Protocol')}
                    </TableHead>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Status')}
                    </TableHead>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Referenced lanes')}
                    </TableHead>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Members')}
                    </TableHead>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Degraded')}
                    </TableHead>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Requests')}
                    </TableHead>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Failures')}
                    </TableHead>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Success rate')}
                    </TableHead>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Cost (converted)')}
                    </TableHead>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Last error kind')}
                    </TableHead>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Last probe')}
                    </TableHead>
                    <TableHead className={HEAD_CLASS_NAME}>
                      {t('Details')}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((item) => {
                    const protocolLabelKey = resolveProtocolLabelKey(
                      consoleChannelsByName.get(item.channel),
                      item.type
                    )
                    const isExpanded = expandedChannel === item.channel
                    const isOpeningDetail =
                      detailName === item.channel && !detailChannel
                    return (
                      <Fragment key={item.channel}>
                        <TableRow>
                          <TableCell>
                            <Button
                              type='button'
                              variant='ghost'
                              size='sm'
                              className='-ml-2 gap-1'
                              aria-expanded={isExpanded}
                              onClick={() =>
                                setExpandedChannel(
                                  isExpanded ? null : item.channel
                                )
                              }
                            >
                              {isExpanded ? (
                                <ChevronDown className='size-3.5' />
                              ) : (
                                <ChevronRight className='size-3.5' />
                              )}
                              <span className='truncate font-medium'>
                                {item.channel}
                              </span>
                            </Button>
                          </TableCell>
                          <TableCell>
                            {protocolLabelKey ? (
                              t(protocolLabelKey)
                            ) : (
                              <span className='text-muted-foreground'>
                                {item.type || '-'}
                              </span>
                            )}
                          </TableCell>
                          <TableCell>
                            <StatusBadge
                              label={
                                item.enabled ? t('Enabled') : t('Disabled')
                              }
                              variant={item.enabled ? 'success' : 'neutral'}
                              size='sm'
                              copyable={false}
                            />
                          </TableCell>
                          <TableCell className='tabular-nums'>
                            {formatNumber(item.lane_count)}
                          </TableCell>
                          <TableCell className='tabular-nums'>
                            {formatMembers(item)}
                          </TableCell>
                          <TableCell>
                            <StatusBadge
                              label={
                                item.degraded ? t('Degraded') : t('Normal')
                              }
                              variant={item.degraded ? 'warning' : 'success'}
                              size='sm'
                              copyable={false}
                            />
                          </TableCell>
                          <TableCell className='tabular-nums'>
                            {formatNumber(item.requests)}
                          </TableCell>
                          <TableCell className='tabular-nums'>
                            {formatNumber(item.failures)}
                          </TableCell>
                          <TableCell className='tabular-nums'>
                            {formatItemSuccessRate(item)}
                          </TableCell>
                          <TableCell className='tabular-nums'>
                            {formatCostYuan(item.estimated_cost)}
                          </TableCell>
                          <TableCell>
                            {item.last_error_kind ? (
                              <span className='font-mono'>
                                {item.last_error_kind}
                              </span>
                            ) : (
                              <span className='text-muted-foreground text-xs'>
                                -
                              </span>
                            )}
                          </TableCell>
                          <TableCell className='whitespace-nowrap tabular-nums'>
                            {formatLastProbe(item.last_probe)}
                          </TableCell>
                          <TableCell>
                            <Button
                              type='button'
                              variant='outline'
                              size='sm'
                              disabled={isOpeningDetail}
                              onClick={() => setDetailName(item.channel)}
                            >
                              {isOpeningDetail && (
                                <Loader2 className='size-3.5 animate-spin' />
                              )}
                              {t('Details')}
                            </Button>
                          </TableCell>
                        </TableRow>
                        {isExpanded && (
                          <TableRow>
                            <TableCell colSpan={13} className='bg-muted/20'>
                              <ChannelStatusRowModels
                                channel={item.channel}
                                timeWindow={timeWindow}
                              />
                            </TableCell>
                          </TableRow>
                        )}
                      </Fragment>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </SectionPageLayout.Content>
      </SectionPageLayout>

      {/* 抽屉挂在布局之外：SectionPageLayout 只渲染具名槽位，其它子节点会被丢弃。
          抽屉的「去检测」复用渠道模块既有的上游变动流程（`useChannels`），因此这里
          按渠道列表页同样的方式提供 provider，两处入口的抽屉行为完全一致。 */}
      {detailChannel && (
        <ChannelsProvider>
          <ChannelStatusDrawer
            open
            onOpenChange={(open) => {
              if (!open) setDetailName(null)
            }}
            channel={detailChannel}
          />
        </ChannelsProvider>
      )}
    </>
  )
}
