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
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ChannelModelPrice } from '../../types'
import { ChannelPriceImportPanel } from '../channel-price-import-panel'

// 批量粘贴导入面板（ui-spec §9）：解析 → 表格预览 → 确认后写回表单；
// 解析失败逐行给出行号与原因，且不提供写入动作。

const CSV_HEADER =
  'model,input,output,cache_read,cache_write,timezone,days,start,end'

afterEach(() => {
  cleanup()
})

function renderPanel(value: ChannelModelPrice[] = []) {
  const onApply = vi.fn()
  render(
    <ChannelPriceImportPanel
      value={value}
      onApply={onApply}
      onClose={() => undefined}
    />
  )
  return onApply
}

function pasteAndParse(text: string) {
  fireEvent.change(screen.getByLabelText('Paste price rows'), {
    target: { value: text },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Parse' }))
}

describe('CSV 导入', () => {
  it('解析后先预览，确认后按模型合并写回表单', () => {
    const onApply = renderPanel([{ model: 'existing', input: 1 }])

    pasteAndParse(
      [
        CSV_HEADER,
        'gpt-4o,10,20,,,Asia/Shanghai,1-5,00:30,08:30',
        'gpt-4o-mini,3,6,,,UTC,,,',
      ].join('\n')
    )

    expect(screen.getByText('Price rows parsed: 2')).toBeVisible()
    expect(screen.getByText('gpt-4o')).toBeVisible()
    expect(screen.getByText('gpt-4o-mini')).toBeVisible()
    // 预览同时显示时段窗口与该时段的显式价格。
    expect(
      screen.getByText('1-5 00:30–08:30 (Input 10 / Output 20)')
    ).toBeVisible()
    expect(onApply).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Apply to form' }))

    expect(onApply).toHaveBeenCalledWith([
      { model: 'existing', input: 1 },
      {
        model: 'gpt-4o',
        timezone: 'Asia/Shanghai',
        bands: [
          {
            days: [1, 2, 3, 4, 5],
            start: '00:30',
            end: '08:30',
            input: 10,
            output: 20,
          },
        ],
      },
      { model: 'gpt-4o-mini', input: 3, output: 6, timezone: 'UTC' },
    ])
    expect(screen.getByText('Price rows imported: 2')).toBeVisible()
  })

  it('解析失败给出行号与原因且不允许写入', () => {
    const onApply = renderPanel()

    pasteAndParse('gpt-4o,10,20,,,UTC,1-5,9:00,18:00')

    expect(screen.getByText('Line 1: Use HH:MM (00:00–23:59)')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Apply to form' })).toBeDisabled()
    expect(onApply).not.toHaveBeenCalled()
  })

  it('空文本时 Parse 不可用', () => {
    renderPanel()

    expect(screen.getByRole('button', { name: 'Parse' })).toBeDisabled()
  })
})

describe('JSON 导入', () => {
  it('价格行数组可直接预览并写入', () => {
    const onApply = renderPanel()

    pasteAndParse(
      JSON.stringify([
        {
          model: 'gpt-4o',
          input: 10,
          bands: [{ days: [1], start: '00:30', end: '08:30', input: 5 }],
        },
      ])
    )

    expect(screen.getByText('Price rows parsed: 1')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: 'Apply to form' }))
    expect(onApply).toHaveBeenCalledWith([
      {
        model: 'gpt-4o',
        input: 10,
        bands: [{ days: [1], start: '00:30', end: '08:30', input: 5 }],
      },
    ])
  })

  it('JSON 语法错误按条目标号提示', () => {
    renderPanel()

    pasteAndParse('[{"model":"a"},]')

    expect(screen.getByText('Row 1: Invalid JSON')).toBeVisible()
  })

  it('修改文本后必须重新解析才能写入', () => {
    renderPanel()

    pasteAndParse(JSON.stringify([{ model: 'gpt-4o', input: 1 }]))
    expect(screen.getByRole('button', { name: 'Apply to form' })).toBeEnabled()

    fireEvent.change(screen.getByLabelText('Paste price rows'), {
      target: { value: JSON.stringify([{ model: 'gpt-4o-mini', input: 2 }]) },
    })
    expect(
      screen.queryByRole('button', { name: 'Apply to form' })
    ).not.toBeInTheDocument()
  })
})
