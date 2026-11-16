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
  formatModelThroughput,
  formatThroughputTps,
  modelThroughputNote,
} from '../format'

describe('formatModelThroughput', () => {
  it('shows TPS only when clean samples exist', () => {
    expect(
      formatModelThroughput({ output_tps: 191.16, tps_samples: 1247 })
    ).toBe('191.2 tok/s')
  })

  it('returns null for failover-only models (no clean samples)', () => {
    // deepseek-flash / deepseek-v4.1-flash 的请求全部经过 3~5 次故障转移，
    // total_attempts > 1，干净样本为 0；服务状态只显示 p50，不出现孤零零的 `-`。
    expect(formatModelThroughput({ output_tps: 0, tps_samples: 0 })).toBeNull()
  })

  it('returns null for zero/embedding output and invalid values', () => {
    expect(
      formatModelThroughput({ output_tps: 0, tps_samples: 1020 })
    ).toBeNull()
    expect(
      formatModelThroughput({ output_tps: Number.NaN, tps_samples: 3 })
    ).toBeNull()
  })

  it('keeps the raw formatter behavior for callers that already gate samples', () => {
    expect(formatThroughputTps(41.2)).toBe('41.2 tok/s')
    expect(formatThroughputTps(0)).toBe('-')
  })
})

describe('modelThroughputNote', () => {
  it('explains why the TPS subline is missing', () => {
    expect(modelThroughputNote({ output_tps: 0, tps_samples: 0 })).toBe(
      'no-clean-samples'
    )
    expect(modelThroughputNote({ output_tps: 0, tps_samples: 1020 })).toBe(
      'no-output-tokens'
    )
    expect(
      modelThroughputNote({ output_tps: 191.16, tps_samples: 1247 })
    ).toBeNull()
  })
})
