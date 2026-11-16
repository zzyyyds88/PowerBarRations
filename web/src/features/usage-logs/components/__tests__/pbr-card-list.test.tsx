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
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'

import { usageLogSchema } from '../../data/schema'
import { PBRLogCardList } from '../pbr-log-card-list'
import { UsageLogsProvider } from '../usage-logs-provider'

const log = usageLogSchema.parse({
  id: 11,
  user_id: 1,
  created_at: 1788840000,
  type: 2,
  content: '',
  model_name: 'request-model',
  channel: 7,
  channel_name: 'upstream-a',
  token_name: 'client-key',
  use_time: 2,
  prompt_tokens: 1200,
  completion_tokens: 800,
  is_stream: true,
  other: JSON.stringify({
    frt: 320,
    pbr: {
      lane: 'lane-a',
      route_source: 'auto',
      upstream_model: 'upstream-model',
      error_kind: '',
      error_summary: '',
      http_status: 200,
      total_ms: 2100,
      estimated_cost: 0.0123,
      attempts: [],
      total_attempts: 1,
      inbound_format: 'openai-chat',
      success: true,
    },
  }),
})

function Fixture() {
  const table = useReactTable({
    data: [log],
    columns: [],
    getCoreRowModel: getCoreRowModel(),
  })
  return (
    <PBRLogCardList
      table={table as never}
      isLoading={false}
      emptyTitle='No logs'
      emptyDescription='Nothing here'
    />
  )
}

it('renders the route chain and operational metrics in the card view', () => {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <UsageLogsProvider>
        <Fixture />
      </UsageLogsProvider>
    </QueryClientProvider>
  )

  expect(screen.getByText('lane-a')).toBeInTheDocument()
  expect(screen.getByText('upstream-a')).toBeInTheDocument()
  expect(screen.getByText('request-model')).toBeInTheDocument()
  expect(screen.getByText('320 ms')).toBeInTheDocument()
  expect(screen.getByText('2.10 s')).toBeInTheDocument()
  expect(screen.getByText('¥0.0123')).toBeInTheDocument()
})
