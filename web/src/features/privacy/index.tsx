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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ShieldCheck } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { WorkspacePage } from '@/components/workspace-page'
import { formatTokens } from '@/lib/format'
import { cn } from '@/lib/utils'

import { usePrivacyColumns } from './components/privacy-columns'
import { PrivacyPreviewCard } from './components/privacy-preview-card'
import { PrivacyRecordDialog } from './components/privacy-record-dialog'
import { PrivacySettingsCard } from './components/privacy-settings-card'
import {
  downloadPrivacyModel,
  getPrivacyModel,
  getPrivacySettings,
  getPrivacySummary,
  listPrivacyRecords,
  updatePrivacySettings,
  type PrivacyRecord,
  type PrivacySettings,
} from './privacy-api'

const ACTION_KINDS = [
  'API_KEY',
  'PRIVATE_KEY',
  'TOKEN',
  'PASSWORD',
  'SECRET',
  'EMAIL',
  'PHONE',
  'ID_CARD',
  'BANK_CARD',
  'SSN',
  'PASSPORT',
  'IBAN',
  'USERNAME',
  'HOSTNAME',
  'IP',
  'BUCKET',
]

function modelState(
  installed: boolean | undefined,
  downloading: boolean | undefined,
  t: (key: string) => string
): string {
  if (installed) return t('Ready')
  if (downloading) return t('Downloading…')
  return t('Rules only')
}

function Kpi(props: { label: string; value: string; tone?: string }) {
  return (
    <div className='border-border/60 bg-muted/25 inline-flex h-7 items-center gap-2 rounded-md border px-2.5 text-xs shadow-xs'>
      <span className='text-muted-foreground whitespace-nowrap'>
        {props.label}
      </span>
      <span
        className={cn(
          'text-foreground/85 font-mono font-semibold tabular-nums',
          props.tone
        )}
      >
        {props.value}
      </span>
    </div>
  )
}

/**
 * Privacy masking (privacy-spec §7, ui-spec §6 日志): configuration, a
 * try-it box and the metadata-only record of what requests masked.
 */
export function PrivacyPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<PrivacySettings | null>(null)
  const [selected, setSelected] = useState<PrivacyRecord | null>(null)
  const [kind, setKind] = useState('')
  const [by, setBy] = useState('')
  const [action, setAction] = useState('')
  const [blocked, setBlocked] = useState(false)

  const settingsQuery = useQuery({
    queryKey: ['privacy-settings'],
    queryFn: getPrivacySettings,
    staleTime: 30_000,
  })
  const modelQuery = useQuery({
    queryKey: ['privacy-model'],
    queryFn: getPrivacyModel,
    refetchInterval: (query) => (query.state.data?.downloading ? 1500 : false),
  })
  const summaryQuery = useQuery({
    queryKey: ['privacy-summary'],
    queryFn: () => getPrivacySummary(7),
    staleTime: 60_000,
  })

  const [pagination, onPaginationChange] = useState({
    pageIndex: 0,
    pageSize: 50,
  })
  const query = useQuery({
    queryKey: [
      'privacy-records',
      kind,
      by,
      action,
      blocked,
      pagination.pageIndex,
      pagination.pageSize,
    ],
    queryFn: () =>
      listPrivacyRecords({
        kind: kind || undefined,
        by: by || undefined,
        action: action || undefined,
        blocked: blocked || undefined,
        page: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      }),
    staleTime: 30_000,
  })

  const settings = draft ?? settingsQuery.data
  const saveMutation = useMutation({
    mutationFn: (next: PrivacySettings) => updatePrivacySettings(next),
    onSuccess: (saved) => {
      setDraft(saved)
      void queryClient.invalidateQueries({ queryKey: ['privacy-settings'] })
      void queryClient.invalidateQueries({ queryKey: ['privacy-model'] })
      void queryClient.invalidateQueries({ queryKey: ['privacy-summary'] })
      void queryClient.invalidateQueries({ queryKey: ['privacy-records'] })
      toast.success(t('Privacy settings saved'))
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : t('Could not save settings')
      )
    },
  })
  const downloadMutation = useMutation({
    mutationFn: downloadPrivacyModel,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['privacy-model'] })
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : t('Could not start download')
      )
    },
  })

  const columns = usePrivacyColumns({ onOpen: setSelected })
  const records = useMemo(() => query.data?.items ?? [], [query.data])
  const { table } = useDataTable({
    data: records,
    columns,
    totalCount: query.data?.total ?? 0,
    getRowId: (row) => String(row.id),
    pagination,
    onPaginationChange,
    manualPagination: true,
    manualFiltering: true,
  })

  if (settingsQuery.isLoading) {
    return (
      <WorkspacePage
        title={t('Masking')}
        description={t(
          'Keep secrets and personal data away from vendors, and see what masking did.'
        )}
      >
        <div className='grid gap-2'>
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className='h-24 w-full rounded-md' />
          ))}
        </div>
      </WorkspacePage>
    )
  }
  if (settingsQuery.isError || !settings) {
    return (
      <WorkspacePage
        title={t('Masking')}
        description={t(
          'Keep secrets and personal data away from vendors, and see what masking did.'
        )}
      >
        <ErrorState
          title={t('Could not load privacy settings')}
          description={
            settingsQuery.error instanceof Error
              ? settingsQuery.error.message
              : undefined
          }
          onRetry={() => void settingsQuery.refetch()}
        />
      </WorkspacePage>
    )
  }

  return (
    <WorkspacePage
      title={t('Masking')}
      description={t(
        'Keep secrets and personal data away from vendors, and see what masking did.'
      )}
    >
      <div className='space-y-3'>
        <div className='flex flex-wrap items-center gap-2'>
          <Kpi
            label={t('Masked values')}
            value={String(summaryQuery.data?.masked_values ?? 0)}
          />
          <Kpi
            label={t('Requests')}
            value={String(summaryQuery.data?.requests ?? 0)}
          />
          <Kpi
            label={t('Blocked')}
            value={String(summaryQuery.data?.blocked ?? 0)}
            tone={(summaryQuery.data?.blocked ?? 0) > 0 ? 'text-rose-600' : ''}
          />
          <Kpi
            label={t('Model')}
            value={modelState(
              modelQuery.data?.installed,
              modelQuery.data?.downloading,
              t
            )}
          />
          <span className='text-muted-foreground text-xs'>
            {t('Last 7 days; window totals, not the current page.')}
          </span>
        </div>

        <div className='grid gap-3 xl:grid-cols-[minmax(0,1fr)_360px]'>
          <PrivacySettingsCard
            settings={settings}
            onChange={setDraft}
            onSave={() => saveMutation.mutate(settings)}
            saving={saveMutation.isPending}
            model={modelQuery.data}
            onDownload={() => downloadMutation.mutate()}
            downloading={downloadMutation.isPending}
          />
          <PrivacyPreviewCard />
        </div>

        <div className='border-border/70 bg-card overflow-hidden rounded-md border'>
          <div className='flex flex-wrap items-center gap-2 border-b px-3 py-2'>
            <span className='text-sm font-semibold'>{t('Records')}</span>
            <span className='text-muted-foreground text-xs'>
              {t('Only kinds, placeholders and hints are kept.')}
            </span>
            <div className='ml-auto flex flex-wrap items-center gap-1.5'>
              <Select
                value={kind || 'all'}
                onValueChange={(value) =>
                  setKind((value ?? 'all') === 'all' ? '' : String(value))
                }
              >
                <SelectTrigger className='h-8 w-32 text-xs'>
                  <SelectValue>{kind || t('All kinds')}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='all'>{t('All kinds')}</SelectItem>
                  {ACTION_KINDS.map((value) => (
                    <SelectItem key={value} value={value}>
                      {value}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select
                value={by || 'all'}
                onValueChange={(value) =>
                  setBy((value ?? 'all') === 'all' ? '' : String(value))
                }
              >
                <SelectTrigger className='h-8 w-28 text-xs'>
                  <SelectValue>{by || t('All sources')}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='all'>{t('All sources')}</SelectItem>
                  <SelectItem value='rule'>{t('Rule')}</SelectItem>
                  <SelectItem value='yours'>{t('Yours')}</SelectItem>
                  <SelectItem value='word'>{t('Word')}</SelectItem>
                  <SelectItem value='model'>{t('Model')}</SelectItem>
                  <SelectItem value='known'>{t('Known')}</SelectItem>
                </SelectContent>
              </Select>
              <Select
                value={action || 'all'}
                onValueChange={(value) =>
                  setAction((value ?? 'all') === 'all' ? '' : String(value))
                }
              >
                <SelectTrigger className='h-8 w-28 text-xs'>
                  <SelectValue>
                    {action ? action : t('All actions')}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='all'>{t('All actions')}</SelectItem>
                  <SelectItem value='warn'>{t('Warn')}</SelectItem>
                  <SelectItem value='block'>{t('Block')}</SelectItem>
                </SelectContent>
              </Select>
              <label className='flex items-center gap-1.5 text-xs'>
                <input
                  type='checkbox'
                  className='accent-primary size-3.5'
                  checked={blocked}
                  onChange={(event) => setBlocked(event.target.checked)}
                />
                {t('Blocked only')}
              </label>
            </div>
          </div>
          <div className='h-[520px]'>
            <DataTablePage
              table={table}
              columns={columns}
              isLoading={query.isLoading}
              isFetching={query.isFetching}
              isError={query.isError}
              error={query.error}
              onRetry={() => void query.refetch()}
              emptyTitle={t('No masking records')}
              emptyDescription={t(
                'Requests that mask values appear here once masking is on.'
              )}
              skeletonKeyPrefix='privacy-record-skeleton'
            />
          </div>
        </div>

        <div className='flex items-center justify-between text-xs'>
          <span className='text-muted-foreground inline-flex items-center gap-1'>
            <ShieldCheck className='size-3' aria-hidden='true' />
            {t('Masking never stores the values or the prompt.')}
          </span>
          <span className='text-muted-foreground'>
            {formatTokens(summaryQuery.data?.masked_values ?? 0)} {t('hits')}
          </span>
        </div>
      </div>

      <PrivacyRecordDialog
        record={selected}
        onClose={() => setSelected(null)}
      />
    </WorkspacePage>
  )
}
