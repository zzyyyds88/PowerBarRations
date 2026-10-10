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

import type { PBRStatBucket } from '../../pbr-stats-api'
import { perMinute, sumWindowTotals, windowMinutes } from '../metrics'
import { formatRequestsPerMinute, formatTokensPerMinute } from '../rate-format'

function bucket(partial: Partial<PBRStatBucket>): PBRStatBucket {
  return {
    bucket_ts: 1_700_000_000,
    group: 'model-a',
    requests: 0,
    successes: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    estimated_cost: 0,
    ...partial,
  }
}

describe('windowMinutes', () => {
  it('converts a positive span to minutes', () => {
    // 24 小时 = 1440 分钟。
    expect(windowMinutes(0, 86_400)).toBe(1440)
  })

  it('collapses a non-positive span to 0 (no window to spread over)', () => {
    expect(windowMinutes(100, 100)).toBe(0)
    expect(windowMinutes(100, 50)).toBe(0)
  })

  it('returns 0 for non-finite bounds instead of NaN', () => {
    expect(windowMinutes(Number.NaN, 100)).toBe(0)
    expect(windowMinutes(0, Number.POSITIVE_INFINITY)).toBe(0)
  })
})

describe('perMinute', () => {
  it('divides the window total by the window minutes', () => {
    expect(perMinute(1440, 1440)).toBe(1)
    expect(perMinute(300, 60)).toBe(5)
  })

  it('returns 0 instead of Infinity/NaN when the window has no minutes', () => {
    // 除零是真实输入（异常 from/to），必须收敛成 0。
    expect(perMinute(100, 0)).toBe(0)
    expect(perMinute(100, -1)).toBe(0)
    expect(perMinute(100, Number.NaN)).toBe(0)
    expect(perMinute(Number.NaN, 60)).toBe(0)
  })
})

describe('sumWindowTotals', () => {
  it('sums cost, requests and prompt+completion tokens across buckets', () => {
    const totals = sumWindowTotals([
      bucket({
        requests: 100,
        prompt_tokens: 1000,
        completion_tokens: 2000,
        estimated_cost: 4,
      }),
      bucket({
        group: 'model-b',
        requests: 50,
        prompt_tokens: 10,
        completion_tokens: 20,
        estimated_cost: 1.5,
      }),
    ])

    expect(totals.requests).toBe(150)
    expect(totals.tokens).toBe(3030)
    expect(totals.cost).toBeCloseTo(5.5)
  })

  it('returns zeros for an empty window', () => {
    expect(sumWindowTotals([])).toEqual({ cost: 0, requests: 0, tokens: 0 })
  })
})

describe('rate formatting', () => {
  it('renders RPM with exactly one decimal place', () => {
    expect(formatRequestsPerMinute(0.79, 'en')).toBe('0.8')
    expect(formatRequestsPerMinute(150 / 1440, 'en')).toBe('0.1')
    expect(formatRequestsPerMinute(12, 'en')).toBe('12.0')
  })

  it('renders TPM with a magnitude suffix above 1000 and a small decimal below', () => {
    expect(formatTokensPerMinute(199_200, 'en')).toBe('199.2K')
    expect(formatTokensPerMinute(286_900_000, 'en')).toBe('286.9M')
    expect(formatTokensPerMinute(420.5, 'en')).toBe('420.5')
  })

  it('renders zero for a window with no rate instead of NaN/Infinity', () => {
    // perMinute 的除零收敛结果（0）落在展示层时必须是可读的 `0` / `0.0`。
    expect(formatRequestsPerMinute(perMinute(100, 0))).toBe('0.0')
    expect(formatTokensPerMinute(perMinute(100, 0))).toBe('0')
    expect(formatRequestsPerMinute(Number.POSITIVE_INFINITY)).toBe('0.0')
    expect(formatTokensPerMinute(Number.NaN)).toBe('0')
  })

  it('accepts the project language tags without throwing (zhCN / zhTW)', () => {
    // 上一轮把 i18next 的非标准 tag 直接喂 Intl，整页 500；这里把回归钉住。
    expect(() => formatRequestsPerMinute(1.5, 'zhCN')).not.toThrow()
    expect(() => formatTokensPerMinute(420.5, 'zhCN')).not.toThrow()
    expect(formatRequestsPerMinute(1.5, 'zhCN')).toBe('1.5')
    // 中文的数量级走「万」，与 lib/format 的共享实现一致。
    expect(formatTokensPerMinute(199_200, 'zhCN')).toBe('19.92 万')
  })
})
