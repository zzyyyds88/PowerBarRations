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
import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useDocumentVisibility, useLogsTailPolling } from '../use-logs-tail'

let visibility: DocumentVisibilityState = 'visible'

function setVisibility(next: DocumentVisibilityState) {
  visibility = next
  act(() => {
    document.dispatchEvent(new Event('visibilitychange'))
  })
}

beforeEach(() => {
  visibility = 'visible'
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    get: () => visibility,
  })
})

afterEach(() => {
  Reflect.deleteProperty(document, 'visibilityState')
})

describe('useDocumentVisibility', () => {
  it('tracks document.visibilityState changes', () => {
    const { result } = renderHook(() => useDocumentVisibility())
    expect(result.current).toBe(true)

    setVisibility('hidden')
    expect(result.current).toBe(false)

    setVisibility('visible')
    expect(result.current).toBe(true)
  })
})

describe('useLogsTailPolling', () => {
  it('returns the cadence while visible and pauses it while hidden', () => {
    const { result } = renderHook(() =>
      useLogsTailPolling({ intervalMs: 2000, onResume: () => undefined })
    )
    expect(result.current.refetchInterval).toBe(2000)

    setVisibility('hidden')
    expect(result.current.refetchInterval).toBe(false)
    expect(result.current.isDocumentVisible).toBe(false)
  })

  it('does not poll while tail is off', () => {
    const { result } = renderHook(() =>
      useLogsTailPolling({ intervalMs: 0, onResume: () => undefined })
    )
    expect(result.current.refetchInterval).toBe(false)
  })

  it('refetches once when a hidden tab becomes visible again', () => {
    const onResume = vi.fn()
    renderHook(() => useLogsTailPolling({ intervalMs: 5000, onResume }))

    setVisibility('hidden')
    expect(onResume).not.toHaveBeenCalled()
    setVisibility('visible')
    expect(onResume).toHaveBeenCalledTimes(1)
  })

  it('refetches once when tail is enabled from Off', () => {
    const onResume = vi.fn()
    const { rerender } = renderHook(
      (props: { intervalMs: number }) =>
        useLogsTailPolling({ intervalMs: props.intervalMs, onResume }),
      { initialProps: { intervalMs: 0 } }
    )

    rerender({ intervalMs: 2000 })
    expect(onResume).toHaveBeenCalledTimes(1)

    // Switching cadence keeps the already running tail without another
    // immediate fetch; pagination/filter changes do not touch this state.
    rerender({ intervalMs: 10000 })
    expect(onResume).toHaveBeenCalledTimes(1)
  })
})
