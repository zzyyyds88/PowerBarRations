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
import { render, screen, cleanup, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it } from 'vitest'

import { MatchRuleHitCell } from '../match-rule-hit-cell'
import { MatchTypeCell } from '../match-type-cell'

// 「匹配类型」列与模型名内联「规则 · 命中数」单元格（ui-spec §6.3）。
// 断言口径：标签文案来自 getNameRuleConfig；命中数只在非精确条目出现，
// 且点击数字可查看 matched_models 清单。

afterEach(() => {
  cleanup()
})

it('renders the four match type labels from the shared name rule config', () => {
  const labels = ['Exact', 'Prefix', 'Contains', 'Suffix']
  labels.forEach((label, rule) => {
    cleanup()
    render(<MatchTypeCell nameRule={rule} />)
    expect(screen.getByText(label)).toBeVisible()
  })
  // 越界值回退到精确档，不抛出、不显示空白。
  cleanup()
  render(<MatchTypeCell nameRule={9} />)
  expect(screen.getByText('Exact')).toBeVisible()
})

it('shows the match rule description on hover without duplicating label text', async () => {
  render(<MatchTypeCell nameRule={1} />)
  const user = userEvent.setup()
  await user.hover(screen.getByText('Prefix'))
  expect(
    await screen.findByText('Match models starting with this name')
  ).toBeVisible()
})

it('renders nothing for exact rows because their hit count is always itself', () => {
  const { container } = render(
    <MatchRuleHitCell
      nameRule={0}
      matchedCount={4}
      matchedModels={['a', 'b', 'c', 'd']}
    />
  )
  expect(container).toBeEmptyDOMElement()
})

it('shows the rule label with the matched count inline and opens the matched model names', async () => {
  render(
    <MatchRuleHitCell
      nameRule={1}
      matchedCount={2}
      matchedModels={['qwen3-max', 'qwen3-mini']}
    />
  )
  expect(screen.getByText(/Prefix ·/)).toBeVisible()
  const trigger = screen.getByRole('button', { name: 'View matched models' })
  expect(within(trigger).getByText('2')).toBeVisible()
  await userEvent.click(trigger)
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByText('qwen3-max')).toBeVisible()
  expect(within(dialog).getByText('qwen3-mini')).toBeVisible()
})

it('shows a plain zero for a rule that hits nothing, without a popover entry', () => {
  render(<MatchRuleHitCell nameRule={2} matchedCount={0} matchedModels={[]} />)
  expect(screen.getByText('Contains · 0')).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'View matched models' })
  ).not.toBeInTheDocument()
})

it('falls back to the model list length when the backend omits matched_count', () => {
  render(<MatchRuleHitCell nameRule={3} matchedModels={['gpt-4o-2024']} />)
  expect(
    within(
      screen.getByRole('button', { name: 'View matched models' })
    ).getByText('1')
  ).toBeVisible()
})
