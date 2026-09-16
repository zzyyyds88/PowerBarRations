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
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import { parseTiersFromExpr } from '../lib/billing-expr'
import { isBreakdownTierMatched } from '../lib/breakdown-tier-match'

describe('breakdown tier matched-row highlight', () => {
  test('uses the recorded unit and fixed price when token and request branches share a label', () => {
    const tiers = parseTiersFromExpr(
      'len < 1000 ? tier("base", p * 2) : tier("base", fixed(0.01))'
    )
    assert.equal(
      tiers.filter((tier) =>
        isBreakdownTierMatched(tier, tiers, 'base', 'request', 0.01)
      ).length,
      1
    )
  })
})
