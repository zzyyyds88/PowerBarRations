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
import { cleanup, render, screen, within } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, it } from 'vitest'

import { formatCostYuan } from '../../lib/cost'
import { apiKeySchema, type ApiKey } from '../../types'
import { ApiKeyCostCell } from '../api-key-cost-cell'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

const key: ApiKey = apiKeySchema.parse({
  id: 7,
  name: 'production',
  key: 'demo********1234',
  status: 1,
  cost: 0,
  expired_time: -1,
  created_time: 0,
  accessed_time: 0,
  model_limits_enabled: false,
})

function renderCost(apiKey: ApiKey) {
  return render(
    <I18nextProvider i18n={i18n}>
      <ApiKeyCostCell apiKey={apiKey} />
    </I18nextProvider>
  )
}

afterEach(() => {
  cleanup()
})

it('renders a zero cost as ¥0', () => {
  renderCost(key)
  expect(screen.getByText('¥0')).toBeVisible()
  expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
})

it('keeps large converted costs in yuan with two decimals', () => {
  renderCost({ ...key, cost: 12345.678 })
  expect(screen.getByText('¥12345.68')).toBeVisible()
})

it('renders sub-cent costs with four decimals instead of collapsing to zero', () => {
  renderCost({ ...key, cost: 0.0032 })
  expect(screen.getByText('¥0.0032')).toBeVisible()
})

it('falls back to ¥0 when the API omits the cost field', () => {
  renderCost({ ...key, cost: undefined as unknown as number })
  expect(screen.getByText('¥0')).toBeVisible()
})

it('labels the mobile card cost with the translated Consumed key', () => {
  render(
    <I18nextProvider i18n={i18n}>
      <ApiKeyCostCell apiKey={{ ...key, cost: 12 }} variant='card' />
    </I18nextProvider>
  )
  const row = screen.getByText('Consumed (¥)').parentElement as HTMLElement
  expect(within(row).getByText('¥12.00')).toBeVisible()
})

it('formats cost values with the shared yuan formatter', () => {
  expect(formatCostYuan(0)).toBe('¥0')
  expect(formatCostYuan(null)).toBe('¥0')
  expect(formatCostYuan(Number.NaN)).toBe('¥0')
  expect(formatCostYuan(1.239)).toBe('¥1.24')
  expect(formatCostYuan(0.00004)).toBe('¥0.0000')
})
