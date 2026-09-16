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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import type { ModelPricingPluginVariant } from '@/features/model-pricing/api'
import { USD_PRICING_CURRENCY } from '@/features/model-pricing/currency'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { usePricingPreferencesStore } from '@/stores/pricing-preferences-store'

import {
  ModelPricingEditorPanel,
  type ModelPricingEditorPanelHandle,
  type ModelRatioData,
} from '../model-pricing-sheet'
import { TaskPluginPricingEditor } from '../task-plugin-pricing-editor'

let client: QueryClient | undefined

afterEach(() => {
  client?.clear()
  useAuthStore.getState().auth.setUser(null)
  localStorage.clear()
  vi.restoreAllMocks()
})


it('keeps long provider names accessible while allowing the tabs to wrap', () => {
  const longName =
    'Provider with a long international service name and a regional deployment designation'
  render(
    <TaskPluginPricingEditor
      variants={[
        {
          plugin_key: 'alpha',
          plugin_name: 'Alpha',
          usage_schema: {},
          configured: '',
          effective: '1',
          compatible: true,
        },
        {
          plugin_key: 'beta',
          plugin_name: longName,
          usage_schema: {},
          configured: '',
          effective: '1',
          compatible: true,
        },
      ]}
      expressions={{}}
      onChange={vi.fn()}
      modelExpression='1'
      currency={USD_PRICING_CURRENCY}
    >
      <span>Default editor</span>
    </TaskPluginPricingEditor>
  )
  expect(screen.getByRole('tablist', { name: 'Provider' })).toHaveClass(
    'flex-wrap'
  )
  expect(screen.getByRole('tab', { name: longName })).toHaveClass(
    'max-w-full',
    'min-w-0'
  )
  expect(screen.getByText(longName)).toHaveAttribute('title', longName)
})

it('lets an administrator remove an unavailable provider’s saved price', async () => {
  const expression = 'u("images")'
  const { ref } = renderEditor(
    {
      name: 'shared',
      billingMode: 'tiered_expr',
      billingExpr: expression,
      pluginBillingExpr: { retired: expression },
    },
    [
      {
        plugin_key: 'retired',
        plugin_name: 'Retired',
        stale: true,
        usage_schema: {},
        configured: expression,
        effective: '',
        compatible: false,
      },
    ]
  )
  const user = userEvent.setup()
  await user.click(screen.getByRole('tab', { name: 'Retired' }))
  expect(
    screen.getByText('This provider is unavailable for this model.')
  ).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Remove saved price' }))
  expect(await ref.current?.commitDraft()).toMatchObject({
    pluginBillingExpr: {},
    billingExpr: expression,
  })
  expect(screen.getByRole('tab', { name: 'Default' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
})

it('shows pending server validation for an expression the browser cannot check', async () => {
  render(
    <TaskPluginPricingEditor
      variants={[
        {
          plugin_key: 'alpha',
          plugin_name: 'Alpha',
          usage_schema: {},
          configured: '',
          effective: '1',
          compatible: true,
        },
      ]}
      expressions={{}}
      onChange={vi.fn()}
      modelExpression='futureFunction()'
      currency={USD_PRICING_CURRENCY}
    >
      <span>Default editor</span>
    </TaskPluginPricingEditor>
  )
  await userEvent.setup().click(screen.getByRole('tab', { name: 'Alpha' }))
  expect(
    screen.getByText('Expression compatibility will be checked when saving.')
  ).toBeVisible()
})

function renderEditor(
  editData: ModelRatioData,
  variants: ModelPricingPluginVariant[],
  dirty = vi.fn()
) {
  usePricingPreferencesStore.setState({ currency: 'USD' })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: [], vendors: [] },
  })
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { effective: {} } },
  })
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const ref = createRef<ModelPricingEditorPanelHandle>()
  render(
    <QueryClientProvider client={client}>
      <ModelPricingEditorPanel
        ref={ref}
        embedded
        editData={editData}
        pluginVariants={variants}
        onDirtyChange={dirty}
      />
    </QueryClientProvider>
  )
  return { ref, dirty }
}

it('shows and removes a single provider override while retaining the per-call default', async () => {
  const expression = 'tier("base", u("seconds") * 0.4)'
  const { ref } = renderEditor(
    {
      name: 'shared',
      billingMode: 'per-request',
      price: '0.25',
      pluginBillingExpr: { alpha: expression },
    },
    [
      {
        plugin_key: 'alpha',
        plugin_name: 'Alpha',
        usage_schema: { seconds: { type: 'number', unit: 'second' } },
        configured: expression,
        effective: expression,
        compatible: true,
      },
    ]
  )
  expect(
    screen.getByRole('tab', { name: 'Per-request (deprecated)' })
  ).toHaveAttribute('aria-selected', 'true')
  const user = userEvent.setup()
  await user.click(screen.getByRole('tab', { name: 'Alpha' }))
  await user.click(
    screen.getByRole('switch', { name: 'Set separately for this provider' })
  )
  expect(screen.getByText('Use model-level pricing')).toBeVisible()
  expect(await ref.current?.commitDraft()).toMatchObject({
    billingMode: 'per-request',
    price: '0.25',
    pluginBillingExpr: {},
  })
})

it('clears dirty state after restoring an override in a different key order', async () => {
  const expression = 'tier("base", u("seconds") * 0.4)'
  const { dirty } = renderEditor(
    {
      name: 'shared',
      billingMode: 'tiered_expr',
      billingExpr: expression,
      pluginBillingExpr: { alpha: expression, beta: expression },
    },
    ['alpha', 'beta'].map((key) => ({
      plugin_key: key,
      plugin_name: key,
      usage_schema: { seconds: { type: 'number', unit: 'second' } },
      configured: expression,
      effective: expression,
      compatible: true,
    }))
  )
  const user = userEvent.setup()
  await user.click(screen.getByRole('tab', { name: 'alpha' }))
  const toggle = screen.getByRole('switch', {
    name: 'Set separately for this provider',
  })
  await user.click(toggle)
  expect(dirty).toHaveBeenLastCalledWith(true)
  await user.click(toggle)
  expect(dirty).toHaveBeenLastCalledWith(false)
})

it('opens task expression pricing when only provider metadata supplies a schema', async () => {
  renderEditor(
    { name: 'shared', billingMode: 'per-token' },
    ['alpha', 'beta'].map((key) => ({
      plugin_key: key,
      plugin_name: key,
      usage_schema: { seconds: { type: 'number', unit: 'second' } },
      configured: '',
      effective: '',
      compatible: false,
    }))
  )
  await waitFor(() =>
    expect(screen.getByRole('tab', { name: 'Expression' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
  )
  expect(screen.getByRole('textbox', { name: 'seconds' })).toBeVisible()
})
