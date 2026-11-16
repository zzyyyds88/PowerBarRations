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
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { ChannelTypeLogo } from './channel-type-badge'

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (name: string) => <svg data-testid={name} />,
}))
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

afterEach(() => {
  cleanup()
})

test('regular providers keep their logo', () => {
  render(<ChannelTypeLogo type={1} />)
  expect(screen.getByTestId('OpenAI.Color')).toBeInTheDocument()
})

test('unknown types fall back to a neutral icon', () => {
  render(<ChannelTypeLogo type={999} />)
  expect(screen.queryByTestId('OpenAI.Color')).not.toBeInTheDocument()
})
