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
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { ChannelPricesEditor } from '../channel-prices-editor'

// 渠道级上游单价编辑器（design-v1 §16.9#7）：表格初始为空，行按需手动添加
// ——可搜索下拉列出渠道模型清单中尚未添加的模型，也允许键入清单外自定义名
// （独立车道名等）；每一行可删除；未添加的模型不折算成本（免费）。

afterEach(() => {
  cleanup()
})

function renderEditor(props?: {
  value?: Array<Record<string, unknown>>
  models?: string[]
  disabled?: boolean
}) {
  const onChange = vi.fn()
  render(
    <ChannelPricesEditor
      value={(props?.value ?? []) as never}
      models={props?.models ?? ['gpt-4o', 'gpt-4o-mini']}
      onChange={onChange}
      disabled={props?.disabled}
    />
  )
  return { onChange }
}

it('starts empty, shows the free hint, and adds a row from the channel model list', () => {
  const { onChange } = renderEditor()

  expect(
    screen.getByText(
      'No models are priced yet. Unpriced models are free (cost 0). Add rows below.'
    )
  ).toBeVisible()

  const input = screen.getByRole('combobox', {
    name: 'Add a model to price',
  })
  fireEvent.change(input, { target: { value: 'gpt-4o' } })
  fireEvent.keyDown(input, { key: 'Enter' })

  expect(screen.getByText('gpt-4o')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Remove gpt-4o' })).toBeVisible()
  expect(onChange).not.toHaveBeenCalled()
})

it('adds a custom model row when typing a name and pressing Enter', () => {
  const { onChange } = renderEditor()

  const input = screen.getByRole('combobox', {
    name: 'Add a model to price',
  })
  fireEvent.change(input, { target: { value: 'my-lane' } })
  fireEvent.keyDown(input, { key: 'Enter' })

  expect(screen.getByText('my-lane')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Remove my-lane' })).toBeVisible()
  // 行先出现；价格在行内输入，onChange 由价格输入触发。
  expect(onChange).not.toHaveBeenCalled()
})

it('ignores duplicate adds of a model that already has a row', () => {
  renderEditor({ value: [{ model: 'my-lane', input: 1 }] })

  const input = screen.getByRole('combobox', {
    name: 'Add a model to price',
  })
  fireEvent.change(input, { target: { value: 'my-lane' } })
  fireEvent.keyDown(input, { key: 'Enter' })

  // 去重后不会追加第二行（草稿被清空，行数不变）。
  expect(screen.getAllByText('my-lane')).toHaveLength(1)
})

it('shows priced rows for models outside the channel list and allows removing them', () => {
  const { onChange } = renderEditor({
    value: [{ model: 'my-lane', input: 2 }],
    models: ['gpt-4o'],
  })

  // 清单外但已配价的行可见（独立车道名），且可删除。
  expect(screen.getByText('my-lane')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Remove my-lane' }))

  expect(onChange).toHaveBeenCalledWith([])
})

it('keeps an edited price value in the row value', () => {
  const { onChange } = renderEditor()

  const input = screen.getByRole('combobox', {
    name: 'Add a model to price',
  })
  fireEvent.change(input, { target: { value: 'gpt-4o' } })
  fireEvent.keyDown(input, { key: 'Enter' })

  fireEvent.change(screen.getByLabelText('gpt-4o input'), {
    target: { value: '2.5' },
  })

  expect(onChange).toHaveBeenCalledWith([{ model: 'gpt-4o', input: 2.5 }])
})

it('hides the add-model input while the editor is disabled', () => {
  renderEditor({ disabled: true })

  expect(
    screen.queryByRole('combobox', { name: 'Add a model to price' })
  ).not.toBeInTheDocument()
})
