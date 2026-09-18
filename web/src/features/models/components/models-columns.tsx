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
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { BadgeListCell } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { listPBRModels, pbrModelsQueryKey } from '@/features/routes/api'
import { getLobeIcon } from '@/lib/lobe-icon'

import { parseModelTags, resolveModelIconKey } from '../lib'
import type { Model } from '../types'
import { DataTableRowActions } from './data-table-row-actions'
import { DescriptionCell } from './description-cell'
import { MatchTypeCell } from './match-type-cell'
import { MatchedCountCell } from './matched-count-cell'
import { useModels } from './models-provider'

// 模型页 = 模型目录（ui-spec §6.3）：列集合为
// 模型（含推断图标）/ 匹配类型 / 命中模型数 / 描述 / 标签 / 操作。
// 「匹配类型 + 命中模型数」即 New API 的"自动匹配"：一条前缀/包含/后缀规则
// 就能覆盖渠道声明的一批模型名，无需逐个建目录记录。
// 仍不出现「渠道与分组」「同步策略」「展示策略」「自定义端点」「ID」「创建/更新时间」
// 「供应商」「可用分组」「计费类型」等列。

export function useModelsColumns(): ColumnDef<Model>[] {
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
      size: 340,
      minSize: 240,
      enableHiding: false,
      meta: { mobileTitle: true },
      cell: ({ row }) => {
        const model = row.original
        return (
          <div className='flex max-w-[340px] min-w-0 items-start gap-2.5 py-1'>
            <span className='mt-1 flex size-6 shrink-0 items-center justify-center'>
              {getLobeIcon(resolveModelIconKey(model), 24)}
            </span>
            <div className='min-w-0 flex-1'>
              <div className='flex min-w-0 items-center gap-1'>
                <Button
                  variant='link'
                  className='text-foreground h-auto min-w-0 shrink justify-start p-0 font-mono text-sm'
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
              </div>
            </div>
          </div>
        )
      },
    },
    {
      accessorKey: 'name_rule',
      header: t('Match Type'),
      size: 110,
      enableSorting: false,
      meta: { mobileHidden: true },
      cell: ({ row }) => <MatchTypeCell nameRule={row.original.name_rule} />,
    },
    {
      id: 'matched_count',
      header: t('Matched models'),
      size: 130,
      enableSorting: false,
      meta: { mobileHidden: true },
      cell: ({ row }) => (
        <MatchedCountCell
          nameRule={row.original.name_rule}
          matchedCount={row.original.matched_count}
          matchedModels={row.original.matched_models}
        />
      ),
    },
    {
      accessorKey: 'description',
      header: t('Description'),
      size: 260,
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
      size: 180,
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
      size: 105,
      meta: { pinned: 'right' as const },
      cell: ({ row }) => <DataTableRowActions row={row} />,
    },
  ]
}
