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
import type { ColumnDef, RowSelectionState } from '@tanstack/react-table'
import { CheckCircle2, Gauge, Loader2, Trash2 } from 'lucide-react'
import {
  useMemo,
  type ChangeEvent,
  type Dispatch,
  type SetStateAction,
} from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePagination,
  DataTableView,
  useDataTable,
} from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import {
  TestModelsBulkActions,
  TestResultCell,
  TestStatusCell,
} from './channel-test-results'
import {
  getTestTableColumnClass,
  type FailureDetailsState,
  type ModelRow,
  type TestResult,
} from './channel-test-types'

interface ChannelTestTableProps {
  models: string[]
  filteredModels: string[]
  successModels: string[]
  failedModels: string[]
  tableData: ModelRow[]
  testResults: Record<string, TestResult>
  testingModels: Set<string>
  isBatchTesting: boolean
  isBatchStopRequested: boolean
  isAnyTesting: boolean
  searchTerm: string
  defaultTestModel: string
  testAllButtonLabel: string
  rowSelection: RowSelectionState
  setRowSelection: Dispatch<SetStateAction<RowSelectionState>>
  pagination: { pageIndex: number; pageSize: number }
  setPagination: Dispatch<
    SetStateAction<{ pageIndex: number; pageSize: number }>
  >
  onSearchChange: (event: ChangeEvent<HTMLInputElement>) => void
  onBatchTest: (models: string[]) => void
  onStopBatchTest: () => void
  onSelectSuccessful: () => void
  onDeleteFailedPrompt: () => void
  onTestSingle: (model: string) => void
  onOpenDetails: (details: FailureDetailsState) => void
}

export function ChannelTestTable(props: ChannelTestTableProps) {
  const { t } = useTranslation()
  const {
    defaultTestModel,
    isBatchTesting,
    testResults,
    testingModels,
    onTestSingle,
    onOpenDetails,
  } = props

  const columns = useMemo<ColumnDef<ModelRow>[]>(
    () => [
      {
        id: 'select',
        header: ({ table }) => (
          <Checkbox
            checked={table.getIsAllRowsSelected()}
            indeterminate={
              table.getIsSomeRowsSelected() && !table.getIsAllRowsSelected()
            }
            onCheckedChange={(value) => table.toggleAllRowsSelected(!!value)}
            aria-label={t('Select all models')}
          />
        ),
        cell: ({ row }) => (
          <Checkbox
            checked={row.getIsSelected()}
            onCheckedChange={(value) => row.toggleSelected(!!value)}
            aria-label={t('Select model {{model}}', {
              model: row.original.model,
            })}
          />
        ),
        enableSorting: false,
        enableHiding: false,
        size: 40,
      },
      {
        accessorKey: 'model',
        header: t('Model'),
        cell: ({ row }) => {
          const model = row.original.model
          const isDefault = defaultTestModel === model

          return (
            <div className='flex w-max items-center gap-2 whitespace-nowrap'>
              <span className='font-medium whitespace-nowrap' title={model}>
                {model}
              </span>
              {isDefault && (
                <StatusBadge
                  label={t('Default')}
                  variant='info'
                  size='sm'
                  copyable={false}
                />
              )}
            </div>
          )
        },
      },
      {
        id: 'status',
        header: t('Status'),
        cell: ({ row }) => {
          const model = row.original.model
          const result = testResults[model]
          return <TestStatusCell result={result} />
        },
        enableSorting: false,
        size: 112,
      },
      {
        id: 'result',
        header: t('Result'),
        cell: ({ row }) => {
          const model = row.original.model
          const result = testResults[model]
          return (
            <TestResultCell
              result={result}
              model={model}
              onOpenDetails={onOpenDetails}
            />
          )
        },
        enableSorting: false,
        size: 320,
      },
      {
        id: 'actions',
        header: t('Actions'),
        cell: ({ row }) => {
          const model = row.original.model
          const isTestingModel = testingModels.has(model)

          return (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    onClick={() => onTestSingle(model)}
                    disabled={isTestingModel || isBatchTesting}
                    aria-label={t('Test Connection')}
                  />
                }
              >
                {isTestingModel ? (
                  <Loader2 className='size-4 animate-spin' />
                ) : (
                  <Gauge className='size-4' />
                )}
              </TooltipTrigger>
              <TooltipContent>{t('Test Connection')}</TooltipContent>
            </Tooltip>
          )
        },
        enableSorting: false,
      },
    ],
    [
      defaultTestModel,
      isBatchTesting,
      t,
      testResults,
      testingModels,
      onTestSingle,
      onOpenDetails,
    ]
  )

  const { table } = useDataTable({
    data: props.tableData,
    columns,
    rowSelection: props.rowSelection,
    pagination: props.pagination,
    enableRowSelection: true,
    getRowId: (row) => row.model,
    onRowSelectionChange: props.setRowSelection,
    onPaginationChange: props.setPagination,
    withFilteredRowModel: false,
    withSortedRowModel: false,
    withFacetedRowModel: false,
  })

  return (
    <div className='space-y-3 max-sm:has-[div[role="toolbar"]]:pb-16'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
        <div className='min-w-0 space-y-2'>
          <p className='text-sm font-medium'>{t('Channel models')}</p>
          <p className='text-muted-foreground text-xs'>
            {t('Select models to run batch tests.')}
          </p>
          <div className='flex flex-wrap items-center gap-2'>
            {props.isBatchTesting ? (
              <Button
                variant='outline'
                size='sm'
                onClick={props.onStopBatchTest}
                disabled={props.isBatchStopRequested}
              >
                {props.isBatchStopRequested
                  ? t('Stopping...')
                  : t('Stop testing')}
              </Button>
            ) : (
              <>
                <Button
                  size='sm'
                  onClick={() => props.onBatchTest(props.filteredModels)}
                  disabled={
                    props.isAnyTesting || props.filteredModels.length === 0
                  }
                >
                  {props.testAllButtonLabel}
                </Button>
                {props.successModels.length > 0 && (
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={props.onSelectSuccessful}
                  >
                    <CheckCircle2 data-icon='inline-start' />
                    {t('Select successful models ({{count}})', {
                      count: props.successModels.length,
                    })}
                  </Button>
                )}
                {props.failedModels.length > 0 && (
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={props.onDeleteFailedPrompt}
                  >
                    <Trash2 data-icon='inline-start' />
                    {t('Delete failed models ({{count}})', {
                      count: props.failedModels.length,
                    })}
                  </Button>
                )}
              </>
            )}
          </div>
        </div>
        <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
          <Input
            placeholder={t('Filter models...')}
            value={props.searchTerm}
            onChange={props.onSearchChange}
            className='sm:w-64'
          />
        </div>
      </div>

      <div className='space-y-3'>
        <DataTableView
          table={table}
          containerClassName='rounded-md'
          containerProps={{
            role: 'region',
            'aria-label': t('Channel models'),
          }}
          tableContainerClassName='max-h-90 overflow-auto **:data-[slot=table-container]:overflow-visible'
          tableClassName='w-max min-w-full table-auto'
          pinnedColumns={[
            {
              columnId: 'actions',
              side: 'right',
              cellClassName: 'bg-popover',
            },
          ]}
          colgroup={
            <colgroup>
              <col className='w-10 min-w-10' />
              <col className='w-auto' />
              <col className='w-28' />
              <col className='w-80' />
              <col className='w-px' />
            </colgroup>
          }
          getColumnClassName={(columnId) => getTestTableColumnClass(columnId)}
          emptyContent={
            props.models.length
              ? t('No models matched your search.')
              : t('This channel has no configured models.')
          }
          emptyCellClassName='text-muted-foreground h-16 text-center text-sm'
        />

        <DataTablePagination table={table} />
      </div>

      <TestModelsBulkActions table={table} />
    </div>
  )
}
