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
import { toast } from 'sonner'

import {
  getServerErrorDetails,
  getServerErrorSources,
  isServerErrorCancelled,
  type ServerErrorDetails,
} from './server-error-message'

const reportedErrors = new WeakSet<object>()

/** Hint is the actionable part; also surface the stable code for support. */
function buildErrorDescription(
  details: ServerErrorDetails
): string | undefined {
  if (details.hint) {
    return details.code ? `${details.hint} (${details.code})` : details.hint
  }
  return details.code
}

/** Also used when a failure has already been presented inline. */
export function markServerErrorHandled(error: unknown): void {
  for (const source of getServerErrorSources(error)) reportedErrors.add(source)
}

export function handleServerError(
  error: unknown,
  fallbackMessage?: string,
  presentation?: { title: string; description?: string }
): void {
  if (isServerErrorCancelled(error)) return
  const sources = getServerErrorSources(error)
  const reported = sources.some((source) => reportedErrors.has(source))
  markServerErrorHandled(error)
  if (reported) return
  const details = getServerErrorDetails(error, fallbackMessage)
  const message = presentation?.title || details.message
  const description =
    presentation?.description ?? buildErrorDescription(details)
  if (description) {
    toast.error(message, { description })
  } else {
    toast.error(message)
  }
}
