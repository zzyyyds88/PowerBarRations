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
/* eslint-disable react-refresh/only-export-components */
import { useQuery } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { ProgressRing } from '@/components/charts'
import { CopyButton } from '@/components/copy-button'
import { BadgeListCell } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatSuccessRate } from '@/features/dashboard/lib/format'
import { getSuccessRateColor } from '@/features/performance-metrics/lib/format'
import { listPBRModels, pbrModelsQueryKey } from '@/features/routes/api'
import { formatTokens } from '@/lib/format'
import { getLobeIcon } from '@/lib/lobe-icon'

import {
  getModelChannelState,
  parseModelTags,
  resolveModelIconKey,
} from '../lib'
import {
  formatPriceRange,
  useModelCatalogSignals,
  type ModelHealthSummary,
  type ModelPriceSummary,
} from '../lib/model-signals'
import type { Model } from '../types'
import { DataTableRowActions } from './data-table-row-actions'
import { DescriptionCell } from './description-cell'
import { MatchRuleHitCell } from './match-rule-hit-cell'
import { MatchTypeCell } from './match-type-cell'
import { useModels } from './models-provider'

// 模型页 = 模型目录（ui-spec §6.3）：列集合为
// 模型（含推断图标 + 内联「规则 · 命中数」）/ 匹配类型 / 已绑定渠道 / 描述 / 标签 / 操作。
// 「匹配类型 + 内联命中数 + 已绑定渠道」即 New API 的"自动匹配"呈现：
// 一条前缀/包含/后缀规则就能覆盖渠道声明的一批模型名，无需逐个建目录记录，
// 且规则命中了多少渠道模型在列表一眼可见。
// 仍不出现「可用分组」「同步策略」「展示策略」「自定义端点」「ID」「创建/更新时间」
// 「供应商」「计费类型」等列（PBR 无用户分组，渠道数不带分组维度）。

function PriceCell(props: { summary: ModelPriceSummary | null }) {
  const { t } = useTranslation()
  if (!props.summary) {
    return <span className='text-muted-foreground text-xs'>—</span>
  }
  const summary = props.summary
  return (
    <TooltipProvider delay={100}>
      <Tooltip>
        <TooltipTrigger
          render={
            <div className='flex min-w-0 flex-col gap-0.5 text-xs tabular-nums' />
          }
        >
          <span className='font-mono'>
            {t('In')} {formatPriceRange(summary.input)}
          </span>
          <span className='text-muted-foreground font-mono'>
            {t('Out')} {formatPriceRange(summary.output)}
          </span>
        </TooltipTrigger>
        <TooltipContent side='top' className='max-w-xs'>
          <div className='space-y-1 text-xs'>
            <p>{t('Current effective upstream price per 1M tokens.')}</p>
            <p>
              {t('Cache read')}: {formatPriceRange(summary.cacheRead)} ·{' '}
              {t('Cache write')}: {formatPriceRange(summary.cacheWrite)}
            </p>
            <p>{t('Channels: {{count}}', { count: summary.channelCount })}</p>
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

function HealthCell(props: { summary: ModelHealthSummary | null }) {
  const { t } = useTranslation()
  if (!props.summary) {
    return <span className='text-muted-foreground text-xs'>—</span>
  }
  const summary = props.summary
  return (
    <div
      className='flex min-w-0 items-center gap-1.5'
      title={t('Last 24 hours · aggregated by upstream model')}
    >
      <ProgressRing
        value={summary.successRate}
        color={getSuccessRateColor(summary.successRate * 100)}
        ariaLabel={`${t('Success rate')} ${formatSuccessRate(summary.successRate)}`}
      />
      <div className='flex min-w-0 flex-col leading-tight'>
        <span className='font-mono text-xs whitespace-nowrap tabular-nums'>
          {formatSuccessRate(summary.successRate)}
        </span>
        <span className='text-muted-foreground text-[10px] whitespace-nowrap tabular-nums'>
          {t('{{count}} requests', { count: summary.requests })}
        </span>
      </div>
    </div>
  )
}

export function useModelsColumns(models: Model[]): ColumnDef<Model>[] {
  const { t } = useTranslation()
  const { setCurrentRow, setOpen } = useModels()
  // 可调用性由车道决定（ADR 0005）：复用路由页的查询与缓存键，写操作会一并失效。
  const { data: routableModels } = useQuery({
    queryKey: pbrModelsQueryKey,
    queryFn: listPBRModels,
  })
  const routableByName = new Map(
    (routableModels ?? []).map((item) => [item.model, item.routable])
  )
  // 只读价格/健康投影：渠道价格唯一来源仍是渠道 Sheet，模型目录不写。
  const signalsByName = useModelCatalogSignals(models)
  return [
    {
      id: 'select',
      header: ({ table }) => (
        <Checkbox
          checked={table.getIsAllPageRowsSelected()}
          indeterminate={table.getIsSomePageRowsSelected()}
          onCheckedChange={(value) =>
            table.toggleAllPageRowsSelected(Boolean(value))
          }
          aria-label={t('Select all')}
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(Boolean(value))}
          aria-label={t('Select {{name}}', { name: row.original.model_name })}
        />
      ),
      size: 40,
      enableSorting: false,
      enableHiding: false,
    },
    {
      accessorKey: 'model_name',
      header: t('Model'),
      size: 260,
      minSize: 220,
      enableHiding: false,
      meta: { mobileTitle: true },
      cell: ({ row }) => {
        const model = row.original
        return (
          <div className='flex max-w-[260px] min-w-0 items-start gap-2.5 py-1'>
            <span className='mt-1 flex size-6 shrink-0 items-center justify-center'>
              {getLobeIcon(resolveModelIconKey(model), 24)}
            </span>
            <div className='min-w-0 flex-1'>
              <div className='flex min-w-0 items-center gap-1'>
                <Button
                  variant='link'
                  // min-h-6 keeps the (primary) name trigger at the 24px minimum
                  // pointer target; h-auto + p-0 alone rendered a 22px box.
                  className='text-foreground h-auto min-h-6 min-w-0 shrink justify-start p-0 font-mono text-sm'
                  title={model.model_name}
                  onClick={() => {
                    setCurrentRow(model)
                    setOpen('update-model')
                  }}
                >
                  <span className='truncate'>{model.model_name}</span>
                </Button>
                <CopyButton
                  value={model.model_name}
                  className='size-6 shrink-0'
                />
              </div>
              <div className='mt-1 flex flex-wrap items-center gap-1.5'>
                {routableByName.get(model.model_name) === true ? (
                  <StatusBadge
                    label={t('Callable')}
                    variant='success'
                    size='sm'
                  />
                ) : (
                  <StatusBadge
                    label={t('Not callable')}
                    variant='danger'
                    size='sm'
                  />
                )}
                {model.id <= 0 && (
                  <span className='text-muted-foreground text-xs'>
                    {t('Missing metadata')}
                  </span>
                )}
                <MatchRuleHitCell
                  nameRule={model.name_rule}
                  matchedCount={model.matched_count}
                  matchedModels={model.matched_models}
                />
              </div>
            </div>
          </div>
        )
      },
    },
    {
      accessorKey: 'name_rule',
      header: t('Match Type'),
      size: 90,
      enableSorting: false,
      meta: { mobileHidden: true },
      cell: ({ row }) => <MatchTypeCell nameRule={row.original.name_rule} />,
    },
    {
      accessorKey: 'context_window',
      header: t('Context window'),
      size: 100,
      enableSorting: false,
      meta: { mobileHidden: true },
      cell: ({ row }) =>
        row.original.context_window && row.original.context_window > 0 ? (
          <span className='font-mono text-xs tabular-nums'>
            {formatTokens(row.original.context_window)}
          </span>
        ) : (
          <span className='text-muted-foreground'>—</span>
        ),
    },
    {
      id: 'connections',
      header: t('Bound channels'),
      size: 100,
      enableSorting: false,
      meta: { mobileHidden: true },
      cell: ({ row }) => {
        const model = row.original
        const state = getModelChannelState(model)
        return (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger
                render={
                  <span
                    tabIndex={0}
                    className='text-muted-foreground block min-w-0 cursor-help truncate text-xs'
                  />
                }
              >
                {t('Channels {{count}}', {
                  count: model.bound_channels?.length ?? 0,
                })}
              </TooltipTrigger>
              <TooltipContent side='top' className='max-w-[280px]'>
                {t(state.description)}
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )
      },
    },
    {
      id: 'price',
      header: t('Price (¥/1M)'),
      size: 130,
      enableSorting: false,
      meta: { mobileHidden: true },
      cell: ({ row }) => (
        <PriceCell
          summary={signalsByName.get(row.original.model_name)?.price ?? null}
        />
      ),
    },
    {
      id: 'health',
      header: t('Health'),
      size: 120,
      minSize: 110,
      enableSorting: false,
      meta: { mobileHidden: true },
      cell: ({ row }) => (
        <HealthCell
          summary={signalsByName.get(row.original.model_name)?.health ?? null}
        />
      ),
    },
    {
      accessorKey: 'description',
      header: t('Description'),
      size: 180,
      enableSorting: false,
      cell: ({ row }) => (
        <DescriptionCell
          modelName={row.original.model_name}
          description={row.original.description ?? ''}
        />
      ),
    },
    {
      accessorKey: 'tags',
      header: t('Tags'),
      size: 120,
      enableSorting: false,
      meta: { mobileHidden: true },
      cell: ({ row }) => (
        <BadgeListCell
          expandable
          max={1}
          items={parseModelTags(row.original.tags ?? '').map((tag) => (
            <StatusBadge key={tag} label={tag} variant='neutral' size='sm' />
          ))}
        />
      ),
    },
    {
      id: 'actions',
      header: t('Actions'),
      enableSorting: false,
      enableHiding: false,
      size: 90,
      meta: { pinned: 'right' as const },
      cell: ({ row }) => <DataTableRowActions row={row} />,
    },
  ]
}
