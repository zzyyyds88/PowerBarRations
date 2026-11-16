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
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { PriceBand } from '../../lib/channel-prices'
import type { ChannelModelPrice } from '../../types'
import { ChannelPricesEditor } from '../channel-prices-editor'

// 峰谷价格 UI（ui-spec §9）：行内折叠面板提供 timezone、有序 bands、周视图、
// 当前生效价与 DeepSeek 结构预设；旧单层价按全天兜底显示且不生成 bands。

type PricedRow = ChannelModelPrice & {
  timezone?: string
  bands?: PriceBand[]
}

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

function renderEditor(initial: PricedRow[]) {
  function Wrapper() {
    const [value, setValue] = useState<PricedRow[]>(initial)
    return (
      <ChannelPricesEditor
        value={value}
        models={[]}
        onChange={(next) => setValue(next)}
      />
    )
  }
  render(<Wrapper />)
}

function expandBands(model = 'gpt-4o') {
  fireEvent.click(
    screen.getByRole('button', {
      name: `Edit peak/off-peak bands for ${model}`,
    })
  )
}

describe('旧单层价兼容', () => {
  it('无 bands 的行显示为全天兜底，price_band 为 -1', () => {
    renderEditor([{ model: 'gpt-4o', input: 10, output: 20 }])

    expect(
      screen.getByRole('button', {
        name: 'Edit peak/off-peak bands for gpt-4o',
      })
    ).toHaveTextContent('All day')
    expect(screen.getByText('price_band -1')).toBeVisible()
    // 旧行不显示任何峰谷时段摘要。
    expect(screen.queryByText('Bands: 1')).not.toBeInTheDocument()
  })

  it('展开后提示行级价全天生效，且不强行生成 bands', () => {
    renderEditor([{ model: 'gpt-4o', input: 10 }])
    expandBands()

    expect(
      screen.getByRole('region', { name: 'Peak/off-peak bands' })
    ).toBeVisible()
    expect(
      screen.getByText('No bands: the row price applies all day.')
    ).toBeVisible()
    expect(screen.getByText('Weekly coverage')).toBeVisible()
  })
})

describe('时段列表编辑', () => {
  it('添加 band 默认七天全选，可切换星期', () => {
    renderEditor([{ model: 'gpt-4o', input: 10 }])
    expandBands()

    fireEvent.click(screen.getByRole('button', { name: 'Add band' }))
    expect(screen.getByText('Band 1')).toBeVisible()

    const monday = screen.getByRole('button', { name: 'Mon' })
    expect(monday).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(monday)
    expect(monday).toHaveAttribute('aria-pressed', 'false')
  })

  it('start > end 时提示跨午夜归起始日', () => {
    renderEditor([
      {
        model: 'gpt-4o',
        input: 10,
        bands: [{ days: [1], start: '22:00', end: '02:00' }],
      },
    ])
    expandBands()

    expect(
      screen.getByText(
        'Spanning midnight: the band belongs to its start day and ends the next day.'
      )
    ).toBeVisible()
    // 周视图：周一 22:00 与周二 01:00 覆盖，周二 02:00 回到兜底。
    expect(screen.getByTitle('Mon 22:00 · Band 1')).toBeInTheDocument()
    expect(screen.getByTitle('Tue 01:00 · Band 1')).toBeInTheDocument()
    expect(
      screen.getByTitle('Tue 02:00 · Row fallback (all day)')
    ).toBeInTheDocument()
  })

  it('按当前时间与时区显示命中的 price_band 下标', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-09T14:00:00Z')) // 周一 22:00 上海
    renderEditor([
      {
        model: 'gpt-4o',
        input: 10,
        timezone: 'Asia/Shanghai',
        bands: [
          { days: [1], start: '22:00', end: '02:00', input: 5 },
          { days: [1, 2, 3, 4, 5, 6, 7], start: '02:00', end: '22:00' },
        ],
      },
    ])
    expandBands()

    const region = screen.getByRole('region', { name: 'Peak/off-peak bands' })
    expect(within(region).getByText(/Now 22:00/)).toBeVisible()
    expect(within(region).getByText(/price_band 0/)).toBeVisible()
    expect(within(region).getByText(/Input 5/)).toBeVisible()
  })
})

describe('就地校验', () => {
  it('空 days、非法 HH:MM 与相等时间都给出提示', () => {
    renderEditor([
      {
        model: 'gpt-4o',
        bands: [{ days: [], start: '09:00', end: '18:00' }],
      },
    ])
    expandBands()

    expect(screen.getByText('Select at least one weekday')).toBeVisible()

    fireEvent.change(screen.getByLabelText('Band 1 start'), {
      target: { value: '9:00' },
    })
    expect(screen.getByText('Use HH:MM (00:00–23:59)')).toBeVisible()

    fireEvent.change(screen.getByLabelText('Band 1 start'), {
      target: { value: '10:00' },
    })
    fireEvent.change(screen.getByLabelText('Band 1 end'), {
      target: { value: '10:00' },
    })
    expect(screen.getByText('Start and end must differ')).toBeVisible()
  })

  it('非法时区就地提示', () => {
    renderEditor([{ model: 'gpt-4o', timezone: 'Mars/Olympus' }])
    expandBands()

    expect(screen.getByText('Invalid time zone')).toBeVisible()
  })
})

describe('DeepSeek 官方峰谷预设', () => {
  it('只填结构：两个时段、七天全选、价格留空', () => {
    renderEditor([{ model: 'gpt-4o' }])
    expandBands()

    fireEvent.click(
      screen.getByRole('button', {
        name: 'DeepSeek peak/off-peak preset',
      })
    )

    expect(screen.getByText('Band 1')).toBeVisible()
    expect(screen.getByText('Band 2')).toBeVisible()
    expect(screen.getByLabelText('Band 1 start')).toHaveValue('00:30')
    expect(screen.getByLabelText('Band 1 end')).toHaveValue('08:30')
    expect(screen.getByLabelText('Band 2 start')).toHaveValue('08:30')
    expect(screen.getByLabelText('Band 2 end')).toHaveValue('00:30')
    // 不写死任何金额：band 价全部留空（继承行级价）。
    for (const label of [
      'Band 1 Input price',
      'Band 1 Output price',
      'Band 1 Cache read price',
      'Band 1 Cache write price',
    ]) {
      expect(screen.getByLabelText(label)).toHaveValue('')
    }
    const mondayButtons = screen.getAllByRole('button', { name: 'Mon' })
    expect(mondayButtons).toHaveLength(2)
    for (const button of mondayButtons) {
      expect(button).toHaveAttribute('aria-pressed', 'true')
    }
  })
})
