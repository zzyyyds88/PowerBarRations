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
import { describe, expect, test } from 'vitest'

import { parseTiersFromExpr } from '../lib/billing-expr'
import { getDynamicPricingSummary } from '../lib/dynamic-price'
import type { PricingModel } from '../types'

function pricingModel(overrides: Partial<PricingModel>): PricingModel {
  return {
    id: 1,
    model_name: 'test-model',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 1,
    enable_groups: ['default'],
    ...overrides,
  }
}

describe('expression price summaries', () => {
  test('keeps request prices unchanged by token units and separates mixed billing units', () => {
    const model = pricingModel({
      billing_mode: 'tiered_expr',
      billing_expr:
        'len < 1000 ? tier("request", fixed(0.01)) : tier("tokens", p * 2 + c * 8)',
    })
    const thousand = getDynamicPricingSummary(model, { tokenUnit: 'K' })
    const million = getDynamicPricingSummary(model, { tokenUnit: 'M' })
    const request = thousand?.primaryEntries.find(
      (entry) => entry.unit === 'request'
    )
    expect(request?.value).toBe(0.01)
    expect(request?.formatted).toBe(
      million?.primaryEntries.find((entry) => entry.unit === 'request')
        ?.formatted
    )
    expect(thousand?.primaryEntries.map((entry) => entry.unit)).toEqual([
      'token',
      'token',
      'request',
    ])
    expect(thousand?.isMixedBilling).toBe(true)
    expect(
      thousand?.primaryEntries.every(
        (entry) => entry.formattedRange === undefined
      )
    ).toBe(true)
    const free = getDynamicPricingSummary(
      pricingModel({
        billing_mode: 'tiered_expr',
        billing_expr: 'tier("free", fixed(0))',
      }),
      { tokenUnit: 'M' }
    )
    expect(free?.primaryEntries).toHaveLength(1)
    expect(free?.primaryEntries[0].value).toBe(0)
  })
  const timeExpression =
    'weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || (hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18)) ? tier("peak", p * 3 + cr * 0.1 + c * 9) : tier("off_peak", p * 1.5 + cr * 0.05 + c * 4.5)'

  test.each([
    ['2026-09-07T09:00:00+08:00', 'peak', [3, 9]],
    ['2026-09-07T12:00:00+08:00', 'off_peak', [1.5, 4.5]],
    ['2026-09-12T09:00:00+08:00', 'off_peak', [1.5, 4.5]],
  ])(
    'adds current-period selection at %s without removing detail rows',
    (now, tier, prices) => {
      const summary = getDynamicPricingSummary(
        pricingModel({
          billing_mode: 'tiered_expr',
          billing_expr: timeExpression,
        }),
        { tokenUnit: 'M', now: new Date(now) }
      )
      expect(summary?.tier?.label).toBe(tier)
      expect(summary?.primaryEntries.map((entry) => entry.value)).toEqual(
        prices
      )
      expect(summary?.tiers.map((entry) => entry.label)).toEqual([
        'peak',
        'off_peak',
      ])
      expect(summary?.isTimePricing).toBe(true)
      expect(
        parseTiersFromExpr(timeExpression).every((entry) =>
          entry.conditionText?.includes('Asia/Shanghai')
        )
      ).toBe(true)
    }
  )

  test('keeps the first token tier inside a time branch and leaves request multipliers separate', () => {
    const base =
      'hour("UTC") < 12 ? (len < 1000 ? tier("first", p * 4 + c * 12) : tier("cheaper", p * 2 + c * 6)) : tier("night", p * 1 + c * 3)'
    const summary = getDynamicPricingSummary(
      pricingModel({
        billing_mode: 'tiered_expr',
        billing_expr: `(${base}) * (header("tier") == "fast" ? 2 : 1)`,
      }),
      { tokenUnit: 'M', now: new Date('2026-09-07T10:00:00Z') }
    )
    expect(summary?.tier?.label).toBe('first')
    expect(summary?.primaryEntries.map((entry) => entry.value)).toEqual([4, 12])
    expect(summary?.hasRequestRules).toBe(true)
    expect(
      summary?.primaryEntries.every((entry) => !entry.formattedRange)
    ).toBe(true)
  })
  test('keeps the first token tier even when a later tier is cheaper', () => {
    const summary = getDynamicPricingSummary(
      pricingModel({
        billing_mode: 'tiered_expr',
        billing_expr:
          'len < 1000 ? tier("first", p * 4 + c * 12) : tier("cheaper", p * 2 + c * 6)',
      }),
      { tokenUnit: 'M' }
    )
    expect(summary?.tier?.label).toBe('first')
    expect(summary?.primaryEntries.map((entry) => entry.value)).toEqual([4, 12])
    expect(
      summary?.primaryEntries.every(
        (entry) => entry.formattedRange === undefined
      )
    ).toBe(true)
  })
  test('preserves a versioned parenthesized base price and its request rule', () => {
    const summary = getDynamicPricingSummary(
      pricingModel({
        billing_mode: 'tiered_expr',
        billing_expr:
          'v1:(tier("base", p * 2 + c * 8)) * (header("x-priority") == "high" ? 2 : 1)',
      }),
      { tokenUnit: 'M' }
    )
    expect(summary?.isSpecialExpression).toBe(false)
    expect(summary?.primaryEntries.map((entry) => entry.value)).toEqual([2, 8])
    expect(summary?.hasRequestRules).toBe(true)
  })
  test.each([
    'tier("custom", max(p * 2 + c * 8, 100))',
    'tier("base", p * 2 + c * 8) * 3',
    'param("premium") ? tier("pro", p * 4 + c * 16) : tier("base", p * 2 + c * 8)',
    'tier("overflow", p * 1e999 + c * 8)',
  ])('does not invent structured prices from %s', (expression) => {
    const summary = getDynamicPricingSummary(
      pricingModel({ billing_mode: 'tiered_expr', billing_expr: expression }),
      { tokenUnit: 'M' }
    )
    expect(summary?.isSpecialExpression).toBe(true)
    expect(summary?.entries).toEqual([])
  })

  test('retains explicit zero token rates while omitting absent categories', () => {
    const summary = getDynamicPricingSummary(
      pricingModel({
        billing_mode: 'tiered_expr',
        billing_expr: 'tier("free", p * 0 + c * 0)',
      }),
      { tokenUnit: 'M' }
    )
    expect(
      summary?.primaryEntries.map((entry) => [entry.field, entry.value])
    ).toEqual([
      ['inputPrice', 0],
      ['outputPrice', 0],
    ])
    expect(summary?.secondaryEntries).toEqual([])
  })

})
