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
import { describe, expect, it } from 'vitest'

import { Dialog } from '@/components/dialog'
import { DIALOG_SIZE_CLASS, type DialogSize } from '@/components/dialog-size'

// ui-spec §6.9：居中弹窗外框尺寸只由档位决定，不随内容长度/页签变化。
// 这里断言的是**稳定行为契约**（固定尺寸 class + 正文单层滚动 + header/footer 钉住），
// 不断言像素值，避免依赖浏览器渲染误差。

function renderDialog(
  size: DialogSize | undefined,
  children = <p>body</p>,
  withFooter = true
) {
  return render(
    <Dialog
      open
      onOpenChange={() => undefined}
      title='Title'
      description='Description'
      size={size}
      footer={withFooter ? <button type='button'>OK</button> : undefined}
    >
      {children}
    </Dialog>
  )
}

describe('centered dialog size contract', () => {
  it('defaults to the md tier when no size is given', () => {
    renderDialog(undefined)
    const dialog = screen.getByRole('dialog')
    for (const cls of DIALOG_SIZE_CLASS.md.split(' ')) {
      expect(dialog.className).toContain(cls)
    }
  })

  it('applies the requested tier fixed frame for every tier', () => {
    for (const size of Object.keys(DIALOG_SIZE_CLASS) as DialogSize[]) {
      const { unmount } = renderDialog(size)
      const dialog = screen.getByRole('dialog')
      for (const cls of DIALOG_SIZE_CLASS[size].split(' ')) {
        expect(dialog.className).toContain(cls)
      }
      // 不再出现上游遗留的自适应松宽写法。
      expect(dialog.className).not.toContain('sm:max-w-2xl')
      expect(dialog.className).not.toContain('sm:max-w-5xl')
      unmount()
    }
  })

  it('keeps the frame fixed when content grows (only the body scrolls)', () => {
    const { rerender } = renderDialog('md', <p>short</p>)
    const before = screen.getByRole('dialog').className

    rerender(
      <Dialog
        open
        onOpenChange={() => undefined}
        title='Title'
        description='Description'
        size='md'
        footer={<button type='button'>OK</button>}
      >
        <div>{Array.from({ length: 200 }, (_, i) => <p key={i}>line {i}</p>)}</div>
      </Dialog>
    )

    // 外框 class 完全不变 = 外框尺寸不随内容变化。
    expect(screen.getByRole('dialog').className).toBe(before)
    // 正文区承担滚动。
    const scroller = screen
      .getByRole('dialog')
      .querySelector('.overflow-y-auto.overscroll-contain')
    expect(scroller).not.toBeNull()
    expect(scroller?.className).toContain('min-h-0')
  })

  it('pins header and footer outside the scrollable body', () => {
    renderDialog('lg')
    const dialog = screen.getByRole('dialog')
    const header = dialog.querySelector('[data-slot="dialog-header"]')
    const footer = dialog.querySelector('[data-slot="dialog-footer"]')
    expect(header?.className).toContain('flex-shrink-0')
    expect(footer?.className).toContain('flex-shrink-0')
  })

  it('sm tier keeps a min-height floor instead of a fixed height', () => {
    renderDialog('sm')
    const dialog = screen.getByRole('dialog')
    const tokens = dialog.className.split(/\s+/)
    expect(dialog.className).toContain('min-h-[160px]')
    // sm 档不设固定高度：不存在恰好以 h-[ 开头的固定高度 token（max-h- 不算）。
    expect(tokens.some((token) => token.startsWith('h-['))).toBe(false)
  })
})
