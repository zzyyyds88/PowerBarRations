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
import { render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { DetailsDialog } from '../dialogs/details-dialog'

function makeLog(
  other: LogOtherData,
  promptTokens = 0,
  completionTokens = 20
): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1,
    type: 2,
    content: '',
    username: 'user',
    token_name: 'token',
    model_name: 'gpt-test',
    quota: 5000,
    prompt_tokens: promptTokens,
    completion_tokens: completionTokens,
    use_time: 0,
    is_stream: false,
    channel: 1,
    channel_name: 'channel-a',
    token_id: 1,
    group: 'default',
    ip: '',
    other: JSON.stringify(other),
    request_id: 'req-1',
    upstream_request_id: '',
  }
}

const queryClients: QueryClient[] = []

function renderDetails(
  other: LogOtherData,
  promptTokens = 0,
  completionTokens = 20
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClients.push(queryClient)
  render(
    <QueryClientProvider client={queryClient}>
      <DetailsDialog
        log={makeLog(other, promptTokens, completionTokens)}
        isAdmin={false}
        isRoot={false}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

function rowValue(label: string): string | null {
  return screen.getByText(label).nextElementSibling?.textContent ?? null
}

afterEach(() => {
  for (const queryClient of queryClients) queryClient.clear()
  queryClients.length = 0
})

describe('localized log details dialog', () => {
  test('shows the token breakdown with cache read and write quantities', () => {
    renderDetails(
      { cache_tokens: 300, cache_creation_tokens: 50, image_cache_tokens: 10 },
      1000
    )

    expect(screen.getByText('Token Breakdown')).toBeInTheDocument()
    expect(rowValue('Input Tokens')).toBe('1,000')
    expect(rowValue('Output Tokens')).toBe('20')
    expect(rowValue('Cache Read')).toBe('300')
    expect(rowValue('Cache Write')).toBe('50')
    expect(rowValue('Image Cache')).toBe('10')
  })

  test('shows the request and actual model for mapped logs', () => {
    renderDetails({
      is_model_mapped: true,
      upstream_model_name: 'upstream-gpt-test',
    })

    expect(screen.getByText('Model Mapping')).toBeInTheDocument()
    expect(rowValue('Request Model')).toBe('gpt-test')
    expect(rowValue('Actual Model')).toBe('upstream-gpt-test')
  })

  test('renders param override audit lines with their localized action', () => {
    renderDetails({ po: ['set temperature=0.2'] })

    expect(screen.getByText('Param Override (1)')).toBeInTheDocument()
    expect(screen.getByText('Set')).toBeInTheDocument()
    expect(screen.getByText('temperature=0.2')).toBeInTheDocument()
  })

  test('shows stream status errors with their reason and messages', () => {
    renderDetails({
      stream_status: {
        status: 'error',
        end_reason: 'upstream closed',
        error_count: 1,
        errors: ['chunk decode failed'],
      },
    })

    const section = screen.getByText('Stream Status').closest('div')
    expect(section).not.toBeNull()
    expect(
      within(section as HTMLElement).getByText('upstream closed')
    ).toBeVisible()
    expect(
      within(section as HTMLElement).getByText('chunk decode failed')
    ).toBeVisible()
  })

  test('never renders quota, billing, group or subscription wording', () => {
    renderDetails({
      model_ratio: 1,
      completion_ratio: 2,
      group_ratio: 1,
      model_price: 0.25,
      billing_mode: 'tiered_expr',
      fixed_price: 0.04,
      matched_tier: 'image',
      usage_facts: { resolution: '720P' },
      billing_source: 'subscription',
      subscription_plan_id: 'plan-1',
    })

    const dialog = screen.getByRole('dialog')
    for (const forbidden of [
      'Billing Details',
      'Total Cost',
      'Group Ratio',
      'Subscription Billing',
      'Usage parameters',
      'Quota',
    ]) {
      expect(within(dialog).queryByText(forbidden)).toBeNull()
    }
  })

  test('omits the token breakdown when no tokens were reported', () => {
    renderDetails({ cache_tokens: 0 }, 0, 0)
    expect(screen.queryByText('Token Breakdown')).toBeNull()
  })
})
