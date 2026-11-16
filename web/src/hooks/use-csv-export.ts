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
/**
 * Management CSV export (api-spec §5.5.2).
 *
 * `/api/channels/export`, `/api/keys/export` and `/api/audit/export` return
 * `text/csv` with `Content-Disposition`. The request goes through the shared
 * axios client so the management credential (and its refresh path) is the
 * same as every other call — a bare `window.location` download would drop it.
 * The blob is saved through the browser's native download UI.
 *
 * Contract errors (row limit / range limit) are read back from the blob and
 * surfaced as a toast; a failed export never looks like a successful one.
 */
import { isAxiosError } from 'axios'
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { api } from '@/lib/api'

export interface CsvExportRequest {
  /** Management endpoint, e.g. `/api/channels/export`. */
  path: string
  /** Query parameters (already localized to the endpoint's contract). */
  params?: Record<string, string | number | undefined>
  /** Filename used when the response has no `Content-Disposition`. */
  fallbackFilename: string
}

interface ExportErrorPayload {
  code?: string
  message?: string
  hint?: string
}

function recordValue(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object'
    ? (value as Record<string, unknown>)
    : undefined
}

function parseExportErrorPayload(value: unknown): ExportErrorPayload | null {
  const record = recordValue(value)
  if (!record) return null
  const nested = recordValue(record.error) ?? record
  const code = typeof nested.code === 'string' ? nested.code : undefined
  const message =
    typeof nested.message === 'string' ? nested.message : undefined
  const hint = typeof nested.hint === 'string' ? nested.hint : undefined
  if (!code && !message && !hint) return null
  return {
    ...(code ? { code } : {}),
    ...(message ? { message } : {}),
    ...(hint ? { hint } : {}),
  }
}

async function readExportErrorPayload(
  error: unknown
): Promise<ExportErrorPayload | null> {
  if (!isAxiosError(error)) return null
  const data: unknown = error.response?.data
  if (typeof Blob !== 'undefined' && data instanceof Blob) {
    try {
      return parseExportErrorPayload(JSON.parse(await data.text()))
    } catch {
      return null
    }
  }
  return parseExportErrorPayload(data)
}

function parseExportFilename(
  contentDisposition: string | null | undefined
): string | undefined {
  if (!contentDisposition) return undefined
  const encoded = /filename\*\s*=\s*UTF-8''([^;]+)/i.exec(contentDisposition)
  if (encoded?.[1]) {
    try {
      return decodeURIComponent(encoded[1].trim().replaceAll(/^"|"$/g, ''))
    } catch {
      return undefined
    }
  }
  const plain = /filename\s*=\s*"?([^";]+)"?/i.exec(contentDisposition)
  return plain?.[1]?.trim() || undefined
}

/** Save an in-memory response through the browser's native download path. */
export function saveBlobAsFile(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.rel = 'noopener'
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
}

/**
 * Download one CSV export and toast a localized failure when it cannot be
 * produced. `isExporting` disables the trigger and announces progress.
 */
export function useCsvExport() {
  const { t } = useTranslation()
  const [isExporting, setIsExporting] = useState(false)

  const exportCsv = useCallback(
    async (request: CsvExportRequest) => {
      if (isExporting) return
      setIsExporting(true)
      try {
        const response = await api.get<Blob>(request.path, {
          params: request.params,
          responseType: 'blob',
          timeout: 0,
        })
        const filename =
          parseExportFilename(response.headers['content-disposition']) ??
          request.fallbackFilename
        saveBlobAsFile(response.data, filename)
      } catch (error) {
        const payload = await readExportErrorPayload(error)
        let message: string | undefined
        if (payload?.code === 'export_too_large') {
          message = t(
            'Too many rows to export. Narrow the time range or filters and try again.'
          )
        } else if (payload?.code === 'export_range_too_large') {
          message = t(
            'Export time ranges are limited to 31 days. Narrow the range and try again.'
          )
        } else {
          message = payload?.message ?? payload?.hint
        }
        // A rejected HTTP response with an unreadable body must not surface
        // axios' raw "request failed with status code" string.
        if (!message && isAxiosError(error) && error.response) {
          message = t('Failed to export')
        }
        toast.error(message || t('Failed to export'))
      } finally {
        setIsExporting(false)
      }
    },
    [isExporting, t]
  )

  return { exportCsv, isExporting }
}
