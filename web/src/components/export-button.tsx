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
import { Download, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useCsvExport, type CsvExportRequest } from '@/hooks/use-csv-export'

interface ExportCsvButtonProps {
  /**
   * The export request, or a factory evaluated on click. A factory keeps
   * time-dependent windows (`Date.now()`) out of render.
   */
  request: CsvExportRequest | (() => CsvExportRequest)
  /** Button label; defaults to “Export CSV”. */
  label?: string
  /** Caller-side gate (e.g. an invalid filter range). */
  disabled?: boolean
}

/**
 * Page-header CSV export trigger (ui-spec §6/§12, api-spec §5.5.2).
 *
 * Outline by design: a page has exactly one filled primary action, and
 * export is never it. The button stays disabled and `aria-busy` while the
 * stream downloads; failures surface as a toast from `useCsvExport`.
 */
export function ExportCsvButton(props: ExportCsvButtonProps) {
  const { t } = useTranslation()
  const { exportCsv, isExporting } = useCsvExport()

  return (
    <Button
      type='button'
      variant='outline'
      size='sm'
      disabled={isExporting || props.disabled === true}
      aria-busy={isExporting}
      onClick={() => {
        const request =
          typeof props.request === 'function' ? props.request() : props.request
        void exportCsv(request)
      }}
    >
      {isExporting ? (
        <Loader2 className='animate-spin' aria-hidden='true' />
      ) : (
        <Download aria-hidden='true' />
      )}
      <span>{props.label ?? t('Export CSV')}</span>
    </Button>
  )
}
