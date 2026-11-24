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

import { CHANNEL_FORM_DEFAULT_VALUES, buildSettingJSON } from '../channel-form'

describe('buildSettingJSON rate limits', () => {
  it('writes pbr_rate_limits and drops empty model rows', () => {
    const json = buildSettingJSON({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      pbr_rate_limits: [
        {
          model: ' model-a ',
          max_concurrency: -1,
          max_rpm: 12,
          queue_limit: 0,
          queue_wait_seconds: 5,
        },
        { model: '  ', max_concurrency: 2, max_rpm: 0 },
      ],
    })
    const parsed = JSON.parse(json)
    expect(parsed.pbr_rate_limits).toEqual([
      {
        model: 'model-a',
        max_concurrency: -1,
        max_rpm: 12,
        queue_limit: 0,
        queue_wait_seconds: 5,
      },
    ])
  })

  it('omits the key when no rows are configured', () => {
    const parsed = JSON.parse(
      buildSettingJSON({ ...CHANNEL_FORM_DEFAULT_VALUES })
    )
    expect(parsed.pbr_rate_limits).toBeUndefined()
  })
})
