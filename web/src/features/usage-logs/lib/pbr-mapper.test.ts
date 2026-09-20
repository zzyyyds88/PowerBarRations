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
import { describe, it, expect } from 'vitest'

import { mapPBRLogToUsageLog, mapPBRLogsResponse } from './pbr-mapper'
import type { PBRRequestLogListItem } from '../pbr/pbr-logs-api'

function makeLog(
  overrides: Partial<PBRRequestLogListItem> = {}
): PBRRequestLogListItem {
  return {
    id: 1,
    ts: '2026-09-14T00:00:00Z',
    lane: 'lane-a',
    request_model: 'model-x',
    route_source: 'explicit',
    channel: 'channel-b',
    channel_id: 7,
    upstream_model: 'model-x',
    key_name: 'client-a',
    token_id: 3,
    user_id: 0,
    username: '',
    type: 2,
    ip: '',
    inbound_format: 'openai',
    success: true,
    http_status: 200,
    error_kind: '',
    error_summary: '',
    prompt_tokens: 1200,
    completion_tokens: 340,
    cache_read_tokens: 10,
    cache_write_tokens: 20,
    reasoning_tokens: 0,
    ttft_ms: 420,
    total_ms: 3100,
    is_stream: true,
    attempts: [
      {
        attempt_num: 1,
        member: 'channel-a/model-x',
        status: 'failed',
        duration_ms: 800,
        error_kind: 'timeout',
        msg: 'stream first event timeout',
      },
      {
        attempt_num: 2,
        member: 'channel-b/model-x',
        status: 'success',
        duration_ms: 2300,
      },
    ],
    total_attempts: 2,
    estimated_cost: 0.02,
    ...overrides,
  }
}

describe('mapPBRLogToUsageLog', () => {
  it('maps PBR fields to UsageLog shape', () => {
    const u = mapPBRLogToUsageLog(makeLog())
    expect(u.id).toBe(1)
    expect(u.token_name).toBe('client-a') // key_name → token_name
    expect(u.model_name).toBe('model-x') // request_model → model_name
    expect(u.channel).toBe(7) // channel_id → channel(id)
    expect(u.channel_name).toBe('channel-b') // channel(name) → channel_name
    expect(u.use_time).toBe(3) // total_ms(3100)/1000
    expect(u.created_at).toBe(1789344000) // Date.parse(ts)/1000
    expect(u.quota).toBe(0) // PBR 无额度
    expect(u.type).toBe(2)
    expect(u.ip).toBe('')
  })

  it('builds other JSON with cache/frt/pbr block', () => {
    const u = mapPBRLogToUsageLog(makeLog())
    const other = JSON.parse(u.other)
    expect(other.cache_tokens).toBe(10) // cache_read_tokens
    expect(other.cache_creation_tokens).toBe(20) // cache_write_tokens
    expect(other.frt).toBe(420) // ttft_ms
    expect(other.pbr.lane).toBe('lane-a')
    expect(other.pbr.estimated_cost).toBe(0.02)
    expect(other.pbr.attempts).toHaveLength(2)
    expect(other.pbr.success).toBe(true)
    // 重试链：attempts.member 数组（供 Channel 列 Popover）
    expect(other.admin_info.use_channel).toEqual([
      'channel-a/model-x',
      'channel-b/model-x',
    ])
  })

  it('flags is_model_mapped when upstream differs from request_model', () => {
    const mapped = mapPBRLogToUsageLog(
      makeLog({ upstream_model: 'real-upstream' })
    )
    const same = mapPBRLogToUsageLog(makeLog({ upstream_model: 'model-x' }))
    expect(JSON.parse(mapped.other).is_model_mapped).toBe(true)
    expect(JSON.parse(same.other).is_model_mapped).toBe(false)
  })

  it('puts error_summary into content for Details fallback', () => {
    const u = mapPBRLogToUsageLog(
      makeLog({ error_summary: 'boom', type: 5, success: false })
    )
    expect(u.content).toBe('boom')
    expect(JSON.parse(u.other).pbr.error_summary).toBe('boom')
  })

  it('handles missing attempts gracefully', () => {
    const u = mapPBRLogToUsageLog(makeLog({ attempts: [] }))
    const other = JSON.parse(u.other)
    expect(other.admin_info.use_channel).toEqual([])
    expect(other.pbr.attempts).toEqual([])
  })
})

describe('mapPBRLogsResponse', () => {
  it('maps items and forwards pagination fields', () => {
    const res = mapPBRLogsResponse({
      items: [makeLog({ id: 1 }), makeLog({ id: 2, key_name: 'k2' })],
      total: 42,
      page: 2,
      page_size: 10,
    })
    expect(res.items).toHaveLength(2)
    expect(res.items[0].id).toBe(1)
    expect(res.items[1].token_name).toBe('k2')
    expect(res.total).toBe(42)
    expect(res.page).toBe(2)
    expect(res.page_size).toBe(10)
  })
})
