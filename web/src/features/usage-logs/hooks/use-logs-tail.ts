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
import { useEffect, useRef, useState } from 'react'

/**
 * React state for `document.visibilityState` (ui-spec §6/§12: the page must
 * pause automatic refresh while hidden and resume immediately when visible).
 */
export function useDocumentVisibility(): boolean {
  const [isVisible, setIsVisible] = useState(
    () =>
      typeof document === 'undefined' || document.visibilityState === 'visible'
  )

  useEffect(() => {
    if (typeof document === 'undefined') return
    const handleVisibilityChange = () => {
      setIsVisible(document.visibilityState === 'visible')
    }
    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  }, [])

  return isVisible
}

interface LogsTailPollingOptions {
  /** `0` keeps tailing off. */
  intervalMs: number
  /** Re-run the query when tailing starts or the tab becomes visible again. */
  onResume: () => void
}

interface LogsTailPollingState {
  isDocumentVisible: boolean
  /** Passed straight to TanStack Query's `refetchInterval`. */
  refetchInterval: number | false
}

/**
 * Owns the request-log tail cadence: Off → no polling; hidden tab → paused;
 * becoming visible (or switching from Off to a cadence) → one immediate
 * refetch before the interval resumes.
 *
 * The interval itself lives in `UsageLogsProvider`, so changing filters or
 * pagination never resets the selected cadence.
 */
export function useLogsTailPolling(
  options: LogsTailPollingOptions
): LogsTailPollingState {
  const isDocumentVisible = useDocumentVisibility()
  const onResumeRef = useRef(options.onResume)
  const previousVisibleRef = useRef(isDocumentVisible)
  const previousIntervalRef = useRef(options.intervalMs)

  useEffect(() => {
    onResumeRef.current = options.onResume
  }, [options.onResume])

  useEffect(() => {
    const becameVisible = isDocumentVisible && !previousVisibleRef.current
    const tailJustEnabled =
      options.intervalMs > 0 && previousIntervalRef.current === 0
    previousVisibleRef.current = isDocumentVisible
    previousIntervalRef.current = options.intervalMs

    if (
      (becameVisible || (tailJustEnabled && isDocumentVisible)) &&
      options.intervalMs > 0
    ) {
      onResumeRef.current()
    }
  }, [isDocumentVisible, options.intervalMs])

  return {
    isDocumentVisible,
    refetchInterval:
      isDocumentVisible && options.intervalMs > 0 ? options.intervalMs : false,
  }
}
