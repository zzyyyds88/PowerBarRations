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
抽屉第 3、4 块（ui-spec §6.4-3/4）：

- 「最近失败」（最多 20 条）：时间 / 车道 / 模型 / 状态码 / `error_kind` / 摘要——
  取该渠道最近失败明细（`route_source=test` 的探活记录不计入）；`error_kind` 按
  api-spec §4.6 取值域原样展示，出现 `upstream_reject` 时附"不代表渠道故障"提示。
- 「探活历史」（最多 20 条）+「立即探活」：按钮调 `POST /api/channels/{name}/test`，
  每次探活落一条记录；完成后由抽屉重取详情，刷新历史与运行态。
*/
import { AlertTriangle, Loader2, PlayCircle, Radar } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import {
  formatDurationMs,
  formatStatusTime,
  hasUpstreamReject,
} from '../../lib/channel-status-display'
import type {
  ChannelStatusProbeRecord,
  ChannelStatusRecentFailure,
} from '../../status-api'

/** `upstream_reject` 是"上游拒绝过这份请求"，不是渠道故障（api-spec §4.6）。 */
function UpstreamRejectNotice() {
  const { t } = useTranslation()
  return (
    <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
      <AlertDescription>
        {t('Upstream rejected the request (not a channel fault).')}
      </AlertDescription>
    </Alert>
  )
}

function ChannelStatusFailuresSection(props: {
  failures: ChannelStatusRecentFailure[]
}) {
  const { t } = useTranslation()

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader
        title={t('Recent failures')}
        icon={<AlertTriangle className='size-4' />}
        iconTone='warning'
      />

      {props.failures.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('No recent failures')}
        </p>
      ) : (
        <Table className='text-xs'>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Time')}</TableHead>
              <TableHead>{t('Lane')}</TableHead>
              <TableHead>{t('Model')}</TableHead>
              <TableHead>{t('Status code')}</TableHead>
              <TableHead>{t('Error kind')}</TableHead>
              <TableHead>{t('Summary')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.failures.map((failure) => (
              <TableRow key={failure.id}>
                <TableCell className='whitespace-nowrap tabular-nums'>
                  {formatStatusTime(failure.ts)}
                </TableCell>
                <TableCell>{failure.lane}</TableCell>
                <TableCell>
                  <span className='flex min-w-0 flex-col'>
                    <span className='truncate'>{failure.model}</span>
                    {failure.upstream_model !== failure.model && (
                      <span
                        data-table-text='secondary'
                        className='text-muted-foreground truncate font-mono'
                      >
                        {failure.upstream_model}
                      </span>
                    )}
                  </span>
                </TableCell>
                <TableCell className='tabular-nums'>
                  {failure.http_status}
                </TableCell>
                <TableCell>
                  {failure.error_kind ? (
                    <span className='font-mono'>{failure.error_kind}</span>
                  ) : (
                    <span className='text-muted-foreground text-xs'>-</span>
                  )}
                </TableCell>
                <TableCell className='max-w-[240px]'>
                  <span
                    className='block truncate'
                    title={failure.error_summary}
                  >
                    {failure.error_summary || '-'}
                  </span>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {hasUpstreamReject(
        props.failures.map((failure) => failure.error_kind)
      ) && <UpstreamRejectNotice />}
    </SideDrawerSection>
  )
}

function ChannelStatusProbesSection(props: {
  probes: ChannelStatusProbeRecord[]
  isProbing: boolean
  onProbe: () => void
}) {
  const { t } = useTranslation()

  return (
    <SideDrawerSection>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <SideDrawerSectionHeader
          title={t('Probe history')}
          icon={<Radar className='size-4' />}
          iconTone='info'
          className='min-w-0 flex-1'
        />
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={props.onProbe}
          disabled={props.isProbing}
        >
          {props.isProbing ? (
            <Loader2 className='size-3.5 animate-spin' />
          ) : (
            <PlayCircle className='size-3.5' />
          )}
          {t('Probe now')}
        </Button>
      </div>

      {props.probes.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('No probe history yet')}
        </p>
      ) : (
        <Table className='text-xs'>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Time')}</TableHead>
              <TableHead>{t('Upstream model')}</TableHead>
              <TableHead>{t('Result')}</TableHead>
              <TableHead>{t('Duration')}</TableHead>
              <TableHead>{t('Status code')}</TableHead>
              <TableHead>{t('Error')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.probes.map((probe) => {
              const succeeded = probe.status === 'success'
              return (
                <TableRow key={`${probe.ts}/${probe.upstream_model}`}>
                  <TableCell className='whitespace-nowrap tabular-nums'>
                    {formatStatusTime(probe.ts)}
                  </TableCell>
                  <TableCell>
                    <span className='block truncate font-mono'>
                      {probe.upstream_model}
                    </span>
                  </TableCell>
                  <TableCell>
                    <StatusBadge
                      label={succeeded ? t('Success') : t('Failed')}
                      variant={succeeded ? 'success' : 'danger'}
                      size='sm'
                      copyable={false}
                    />
                  </TableCell>
                  <TableCell className='whitespace-nowrap tabular-nums'>
                    {formatDurationMs(probe.duration_ms)}
                  </TableCell>
                  <TableCell className='tabular-nums'>
                    {probe.http_status}
                  </TableCell>
                  <TableCell className='max-w-[200px]'>
                    <span
                      className='block truncate'
                      title={probe.msg || probe.error_kind}
                    >
                      {probe.msg || probe.error_kind || '-'}
                    </span>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}
    </SideDrawerSection>
  )
}

export function ChannelStatusHistorySections(props: {
  failures: ChannelStatusRecentFailure[]
  probes: ChannelStatusProbeRecord[]
  isProbing: boolean
  onProbe: () => void
}) {
  return (
    <>
      <ChannelStatusFailuresSection failures={props.failures} />
      <ChannelStatusProbesSection
        probes={props.probes}
        isProbing={props.isProbing}
        onProbe={props.onProbe}
      />
    </>
  )
}
