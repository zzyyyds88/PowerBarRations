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
「模型状态」表（api-spec §6.12 的 `models[]`）：一行一个上游模型，展示窗口内的
请求 / 成功率 / 平均延迟 / 平均首字延迟 / 输出速度（tokens/s）/ 输出 token。

这是渠道状态的主内容：抽屉里作为独立一块（当前状态之后、窗口概览之前），
状态页里作为行内可展开表复用同一实现。无样本（`requests = 0`）与 `output_tps = 0`
一律显示 `-`，不显示 0 误导——"还没数据"和"速度是 0"是两回事。
*/
import { Gauge } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatNumber } from '@/lib/format'

import {
  formatDurationMs,
  formatModelSuccessRate,
  formatOutputTps,
} from '../../lib/channel-status-display'
import type { ChannelStatusModelStat } from '../../status-api'

const HEAD_CLASS_NAME = 'whitespace-nowrap'

/** 模型状态表本体（抽屉与状态页行内展开共用，列与口径只有这一处）。 */
export function ChannelStatusModelsTable(props: {
  models: ChannelStatusModelStat[]
}) {
  const { t } = useTranslation()

  if (props.models.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('No model data in this window')}
      </p>
    )
  }

  return (
    <Table className='text-xs'>
      <TableHeader>
        <TableRow>
          <TableHead className={HEAD_CLASS_NAME}>{t('Model')}</TableHead>
          <TableHead className={HEAD_CLASS_NAME}>{t('Requests')}</TableHead>
          <TableHead className={HEAD_CLASS_NAME}>{t('Success rate')}</TableHead>
          <TableHead className={HEAD_CLASS_NAME}>
            {t('Average duration')}
          </TableHead>
          <TableHead className={HEAD_CLASS_NAME}>
            {t('Average first token latency')}
          </TableHead>
          <TableHead className={HEAD_CLASS_NAME}>
            {t('Output speed (tps)')}
          </TableHead>
          <TableHead className={HEAD_CLASS_NAME}>
            {t('Output tokens')}
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {props.models.map((model) => (
          <TableRow key={model.model}>
            <TableCell>
              <span className='block truncate font-mono'>{model.model}</span>
            </TableCell>
            <TableCell className='tabular-nums'>
              {formatNumber(model.requests)}
            </TableCell>
            <TableCell className='tabular-nums'>
              {formatModelSuccessRate(model)}
            </TableCell>
            <TableCell className='whitespace-nowrap tabular-nums'>
              {formatDurationMs(model.avg_total_ms)}
            </TableCell>
            <TableCell className='whitespace-nowrap tabular-nums'>
              {formatDurationMs(model.avg_ttft_ms)}
            </TableCell>
            <TableCell className='tabular-nums'>
              {formatOutputTps(model.output_tps)}
            </TableCell>
            <TableCell className='tabular-nums'>
              {formatNumber(model.tokens_out)}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

/** 抽屉里的「模型状态」分节（标题 + 同一张表）。 */
export function ChannelStatusModelsSection(props: {
  models: ChannelStatusModelStat[]
}) {
  const { t } = useTranslation()

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader
        title={t('Model status')}
        icon={<Gauge className='size-4' />}
        iconTone='chart-1'
      />
      <ChannelStatusModelsTable models={props.models} />
    </SideDrawerSection>
  )
}
