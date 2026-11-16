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
 * Request-log export contract (api-spec §5.5.1).
 *
 * The browser fetches the stream through the shared axios client so the
 * Authorization header (and the interceptor's refresh path) is identical to
 * every other management call; a bare `window.location` download would drop
 * that header. The response is kept in memory as a Blob and handed to the
 * browser's native download UI.
 */
import { isAxiosError } from 'axios'

import { api } from '@/lib/api'

import { getDefaultTimeRange } from './utils'

export type LogsExportFormat = 'csv' | 'ndjson'

export interface LogsExportParams {
  format: LogsExportFormat
  /** RFC3339, required by the backend. */
  from: string
  /** RFC3339, required by the backend. Must be at most 31 days after `from`. */
  to: string
  lane?: string
  channel?: string
  key?: string
  model?: string
  client?: string
  session?: string
  success?: 'true' | 'false'
}

export interface LogsExportProgress {
  loaded: number
  total?: number
  /** 0-100 when the response advertises a total length. */
  percent?: number
}

export interface LogsExportErrorPayload {
  code?: string
  message?: string
  hint?: string
}

function readString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed === '' ? undefined : trimmed
}

function readTime(value: unknown): number | undefined {
  if (value === undefined || value === null || value === '') return undefined
  const parsed = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined
}

/**
 * Turn the page's applied URL filters into the export query. The list and the
 * export share the same filter vocabulary; the only renames are
 * `startTime`/`endTime` → `from`/`to` (RFC3339).
 */
export function buildLogsExportParams(
  search: Record<string, unknown>,
  format: LogsExportFormat
): LogsExportParams {
  const fallback = getDefaultTimeRange()
  const from = readTime(search.startTime) ?? fallback.start.getTime()
  const to = readTime(search.endTime) ?? fallback.end.getTime()
  const lane = readString(search.lane)
  const channel = readString(search.channel)
  const key = readString(search.key)
  const model = readString(search.model)
  const client = readString(search.client)
  const session = readString(search.session)
  const success = readString(search.success)
  return {
    format,
    from: new Date(from).toISOString(),
    to: new Date(to).toISOString(),
    ...(lane ? { lane } : {}),
    ...(channel ? { channel } : {}),
    ...(key ? { key } : {}),
    ...(model ? { model } : {}),
    ...(client ? { client } : {}),
    ...(session ? { session } : {}),
    ...(success === 'true' || success === 'false' ? { success } : {}),
  }
}

/** Parse `Content-Disposition` without assuming a filename is present. */
export function parseExportFilename(
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

export function buildFallbackExportFilename(params: LogsExportParams): string {
  const extension = params.format === 'ndjson' ? 'ndjson' : 'csv'
  return `pbr-logs-${params.from.slice(0, 10)}-${params.to.slice(0, 10)}.${extension}`
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
 * GET /api/logs/export and save the response. Errors keep their original
 * shape so the caller can localize contract codes (e.g. `export_too_large`).
 */
export async function downloadLogsExport(
  params: LogsExportParams,
  onProgress?: (progress: LogsExportProgress) => void
): Promise<void> {
  const response = await api.get<Blob>('/api/logs/export', {
    params,
    responseType: 'blob',
    timeout: 0,
    onDownloadProgress: (event) => {
      if (!onProgress) return
      const total =
        typeof event.total === 'number' && event.total > 0
          ? event.total
          : undefined
      onProgress({
        loaded: event.loaded,
        total,
        percent: total
          ? Math.min(100, Math.round((event.loaded / total) * 100))
          : undefined,
      })
    },
  })
  const filename =
    parseExportFilename(response.headers['content-disposition']) ??
    buildFallbackExportFilename(params)
  saveBlobAsFile(response.data, filename)
}

function recordValue(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object'
    ? (value as Record<string, unknown>)
    : undefined
}

/**
 * The error body is `{error:{code,message,hint}}` per api-spec §2, but accept
 * the flat variant used by some legacy surfaces as well.
 */
export function parseLogsExportErrorPayload(
  value: unknown
): LogsExportErrorPayload | null {
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

/**
 * A Blob-typed response hides the JSON error body from the shared axios
 * interceptor; read it back so the toast can show the contract message.
 */
export async function readLogsExportErrorPayload(
  error: unknown
): Promise<LogsExportErrorPayload | null> {
  if (!isAxiosError(error)) return null
  const data: unknown = error.response?.data
  if (typeof Blob !== 'undefined' && data instanceof Blob) {
    try {
      return parseLogsExportErrorPayload(JSON.parse(await data.text()))
    } catch {
      return null
    }
  }
  return parseLogsExportErrorPayload(data)
}
