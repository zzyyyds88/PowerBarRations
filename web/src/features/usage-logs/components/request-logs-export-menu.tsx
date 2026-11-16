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
import { isAxiosError } from 'axios'
import { Download, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  buildLogsExportParams,
  downloadLogsExport,
  readLogsExportErrorPayload,
  type LogsExportFormat,
} from '../lib/logs-export'

interface RequestLogsExportMenuProps {
  /** Applied URL filters; drafts that were not searched are intentionally absent. */
  search: Record<string, unknown>
}

/**
 * Page-header CSV / NDJSON export (ui-spec §6 日志).
 *
 * The backend owns the 31-day and 100,000-row limits, so the UI forwards the
 * current filters and surfaces the contract error instead of duplicating those
 * rules. The trigger stays disabled and announced while the stream downloads.
 */
export function RequestLogsExportMenu(props: RequestLogsExportMenuProps) {
  const { t } = useTranslation()
  const [exporting, setExporting] = useState<LogsExportFormat | null>(null)
  const [progressPercent, setProgressPercent] = useState<number | null>(null)

  const handleExport = async (format: LogsExportFormat) => {
    if (exporting) return
    setExporting(format)
    setProgressPercent(null)
    try {
      const params = buildLogsExportParams(props.search, format)
      await downloadLogsExport(params, (progress) => {
        setProgressPercent(progress.percent ?? null)
      })
    } catch (error) {
      const payload = await readLogsExportErrorPayload(error)
      let message = payload?.message ?? payload?.hint
      if (!message && payload?.code === 'export_too_large') {
        message = t(
          'Too many rows to export. Narrow the time range or filters and try again.'
        )
      }
      // A rejected HTTP response with an unreadable body must not surface the
      // axios "request failed with status code" string; a transport failure
      // still keeps its own message.
      if (!message && isAxiosError(error) && error.response) {
        message = t('Failed to export logs')
      }
      toast.error(
        message || getServerErrorMessage(error, t('Failed to export logs'))
      )
    } finally {
      setExporting(null)
      setProgressPercent(null)
    }
  }

  let label = t('Export')
  if (exporting) {
    label =
      progressPercent === null
        ? t('Exporting…')
        : t('Exporting… {{percent}}%', { percent: progressPercent })
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={exporting !== null}
            aria-busy={exporting !== null}
          />
        }
      >
        {exporting ? (
          <Loader2 className='animate-spin' aria-hidden='true' />
        ) : (
          <Download aria-hidden='true' />
        )}
        <span aria-live='polite'>{label}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end' className='w-44'>
        <DropdownMenuItem
          disabled={exporting !== null}
          onClick={() => void handleExport('csv')}
        >
          {t('Export as CSV')}
        </DropdownMenuItem>
        <DropdownMenuItem
          disabled={exporting !== null}
          onClick={() => void handleExport('ndjson')}
        >
          {t('Export as NDJSON')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
