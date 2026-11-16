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
import { describe, expect, it } from 'vitest'

import {
  buildFallbackExportFilename,
  buildLogsExportParams,
  parseExportFilename,
  parseLogsExportErrorPayload,
} from '../lib/logs-export'

describe('buildLogsExportParams', () => {
  it('maps the applied list filters to the export contract', () => {
    const params = buildLogsExportParams(
      {
        lane: 'lane-a',
        channel: 'channel-a',
        key: 'key-a',
        model: 'model-a',
        success: 'false',
        startTime: Date.UTC(2026, 9, 1, 0, 0, 0),
        endTime: Date.UTC(2026, 9, 2, 0, 0, 0),
      },
      'ndjson'
    )

    expect(params).toEqual({
      format: 'ndjson',
      from: '2026-10-01T00:00:00.000Z',
      to: '2026-10-02T00:00:00.000Z',
      lane: 'lane-a',
      channel: 'channel-a',
      key: 'key-a',
      model: 'model-a',
      success: 'false',
    })
  })

  it('omits blank filters and invalid success values', () => {
    const params = buildLogsExportParams(
      {
        lane: '  ',
        success: 'all',
        startTime: Date.UTC(2026, 9, 1),
        endTime: Date.UTC(2026, 9, 2),
      },
      'csv'
    )

    expect(params).toEqual({
      format: 'csv',
      from: '2026-10-01T00:00:00.000Z',
      to: '2026-10-02T00:00:00.000Z',
    })
  })

  it('falls back to the default range when the URL has no time bounds', () => {
    const params = buildLogsExportParams({}, 'csv')
    const from = Date.parse(params.from)
    const to = Date.parse(params.to)

    expect(Number.isFinite(from)).toBe(true)
    expect(Number.isFinite(to)).toBe(true)
    expect(to).toBeGreaterThan(from)
    expect(to - from).toBeLessThan(31 * 24 * 60 * 60 * 1000)
  })
})

describe('export filenames', () => {
  it('reads a quoted Content-Disposition filename', () => {
    expect(
      parseExportFilename(
        'attachment; filename="pbr-logs-2026-10-01-2026-10-02.csv"'
      )
    ).toBe('pbr-logs-2026-10-01-2026-10-02.csv')
  })

  it('decodes the RFC 5987 filename* form', () => {
    expect(
      parseExportFilename(
        "attachment; filename*=UTF-8''pbr-logs-%E4%B8%AD%E6%96%87.ndjson"
      )
    ).toBe('pbr-logs-中文.ndjson')
  })

  it('builds a fallback filename from the range and format', () => {
    expect(
      buildFallbackExportFilename({
        format: 'ndjson',
        from: '2026-10-01T00:00:00.000Z',
        to: '2026-10-02T00:00:00.000Z',
      })
    ).toBe('pbr-logs-2026-10-01-2026-10-02.ndjson')
  })
})

describe('parseLogsExportErrorPayload', () => {
  it('reads the nested contract error body', () => {
    expect(
      parseLogsExportErrorPayload({
        error: {
          code: 'export_too_large',
          message: 'matched 120000 rows',
          hint: 'narrow the range',
        },
      })
    ).toEqual({
      code: 'export_too_large',
      message: 'matched 120000 rows',
      hint: 'narrow the range',
    })
  })

  it('reads the flat legacy error body', () => {
    expect(
      parseLogsExportErrorPayload({
        code: 'validation_failed',
        message: 'from is required',
      })
    ).toEqual({ code: 'validation_failed', message: 'from is required' })
  })

  it('returns null for non-error payloads', () => {
    expect(parseLogsExportErrorPayload('nope')).toBeNull()
    expect(parseLogsExportErrorPayload({ ok: true })).toBeNull()
  })
})
