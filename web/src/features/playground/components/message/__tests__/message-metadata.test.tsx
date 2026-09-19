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
import { render, screen } from '@testing-library/react'
import i18next from 'i18next'
import { beforeAll, describe, expect, test } from 'vitest'

import type { Message } from '../../../types'
import { MessageMetadata } from '../message-metadata'

const servedByLabel = 'Served by: {{servedBy}}'
const responseTimeLabel = 'Response time: {{duration}}'

function assistantMessage(overrides: Partial<Message>): Message {
  return {
    key: 'msg-1',
    from: 'assistant',
    versions: [{ id: 'v1', content: 'hi' }],
    ...overrides,
  }
}

describe('MessageMetadata X-Served-By（ui-spec §6.8）', () => {
  beforeAll(() => {
    i18next.addResourceBundle('en', 'translation', {
      [servedByLabel]: servedByLabel,
      [responseTimeLabel]: responseTimeLabel,
    })
  })

  test('消息带 servedBy 时展示实际命中的上游', () => {
    render(
      <MessageMetadata
        alignment='left'
        message={assistantMessage({
          servedBy: 'channel=2:channel-b, model=model-y',
        })}
      />
    )

    expect(
      screen.getByText('Served by: channel=2:channel-b, model=model-y')
    ).toBeInTheDocument()
  })

  test('无 servedBy 时不渲染该文案', () => {
    render(
      <MessageMetadata
        alignment='left'
        message={assistantMessage({ durationMs: 1200 })}
      />
    )

    expect(screen.queryByText(/Served by/)).not.toBeInTheDocument()
    expect(
      screen.getByText(
        'Response time: {{duration}}'.replace('{{duration}}', '1.20s')
      )
    ).toBeInTheDocument()
  })

  test('servedBy 与耗时同时存在时一并展示', () => {
    render(
      <MessageMetadata
        alignment='left'
        message={assistantMessage({
          durationMs: 800,
          servedBy: 'channel=1:channel-a, model=model-x',
        })}
      />
    )

    expect(
      screen.getByText('Served by: channel=1:channel-a, model=model-x')
    ).toBeInTheDocument()
    expect(screen.getByText('Response time: 800ms')).toBeInTheDocument()
  })
})
