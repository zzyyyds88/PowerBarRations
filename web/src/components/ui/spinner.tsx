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
import { cn } from '@/lib/utils'

type SpinnerProps = Omit<React.ComponentProps<'span'>, 'children'> & {
  /** Kept for API compatibility with the previous icon-rendered spinner. */
  strokeWidth?: number
}

/*
 * Conic-gradient loading ring (`.spinner-ring` in index.css): the arc is the
 * gradient, the silhouette is a radial mask, so the spinner is a single
 * element and follows `--primary` on every theme and preset.
 */
function Spinner({ className, strokeWidth, ...props }: SpinnerProps) {
  void strokeWidth
  return (
    <span
      role='status'
      aria-label='Loading'
      className={cn('spinner-ring block size-4 animate-spin', className)}
      {...props}
    />
  )
}

export { Spinner }
