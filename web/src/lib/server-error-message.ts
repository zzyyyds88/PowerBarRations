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
import { isCancelledError } from '@tanstack/react-query'
import { isCancel } from 'axios'
import i18next from 'i18next'

export const safeServerErrorMessage = Symbol('safeServerErrorMessage')

const serverErrorMessageKeys = {
  AUTH_INTERNAL_ERROR: 'Please try again later.',
  AUTH_SESSION_LIMIT:
    'Too many active login sessions. On a device where you are already signed in, open Login sessions and use “Sign out other sessions” to revoke them. If you cannot access a signed-in device, reset your password to sign out all sessions.',
  AUTH_SESSION_ISSUANCE_LIMIT:
    'Too many login sessions were created recently. Please wait for the rolling window to pass, then try again.',
} as const

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object'
}

/** Walk only error origins, never request configs or arbitrary response data. */
export function getServerErrorSources(
  value: unknown
): Record<string | symbol, unknown>[] {
  const sources: Record<string | symbol, unknown>[] = []
  const pending = [value]
  const seen = new Set<object>()
  for (let index = 0; index < pending.length; index++) {
    const source = pending[index]
    if (!isRecord(source) || seen.has(source)) continue
    seen.add(source)
    sources.push(source)
    if (isRecord(source.response)) pending.push(source.response.data)
    pending.push(source.cause, source.error)
  }
  return sources
}

export function getServerErrorMessageKey(value: unknown): string | null {
  for (const source of getServerErrorSources(value)) {
    if (typeof source.code === 'string') {
      const key =
        serverErrorMessageKeys[
          source.code as keyof typeof serverErrorMessageKeys
        ]
      if (key) return key
    }
    if (source[safeServerErrorMessage]) break
  }
  return null
}

export function getServerErrorStatus(value: unknown): number | undefined {
  for (const source of getServerErrorSources(value)) {
    const status = isRecord(source.response)
      ? source.response.status
      : source.status
    if (typeof status === 'number') return status
  }
  return undefined
}

export function isServerErrorCancelled(value: unknown): boolean {
  return getServerErrorSources(value).some(
    (source) =>
      isCancel(source) ||
      isCancelledError(source) ||
      source.name === 'AbortError'
  )
}

function messageText(value: unknown): string | undefined {
  if (
    typeof value !== 'string' ||
    !value.trim() ||
    value.trim() === 'success'
  ) {
    return undefined
  }
  // A proxy's HTML error document is not an actionable API error message.
  if (/^\s*(?:<!doctype|<html[\s>])/i.test(value)) return undefined
  return value
}

export function getServerErrorMessage(
  value: unknown,
  fallback?: string
): string {
  const sources = getServerErrorSources(value)
  // AuthOperationError deliberately hides internal server details. Never unwrap
  // its cause for presentation, even when a caller wraps it in another Error.
  const safe = sources.find((source) => source[safeServerErrorMessage])
  if (safe) {
    return i18next.t(
      getServerErrorMessageKey(safe) ||
        messageText(safe.message) ||
        fallback ||
        'Something went wrong!'
    )
  }
  const key = getServerErrorMessageKey(value)
  if (key) return i18next.t(key)
  for (const source of sources) {
    if (
      source instanceof Error ||
      source.isAxiosError ||
      isRecord(source.response)
    ) {
      continue
    }
    const detail = isRecord(source.error)
      ? messageText(source.error.message)
      : messageText(source.error)
    const message =
      messageText(source.message) || detail || messageText(source.title)
    if (message) return message
  }
  const status = getServerErrorStatus(value)
  if (status === 304) return i18next.t('Content not modified!')
  if (status === 204) return i18next.t('Content not found.')
  for (const source of sources) {
    const message = messageText(source.message)
    if (message) return message
  }
  return messageText(value) || (fallback ?? i18next.t('Something went wrong!'))
}

/** Preserve the payload/cause so every layer can recognize the same failure. */
export function createServerError(value: unknown, fallback?: string): Error {
  return new Error(getServerErrorMessage(value, fallback ?? ''), {
    cause: value,
  })
}

/** Queries reject failed business responses without changing the raw API contract. */
export function requireServerSuccess<T>(response: T): T {
  if (isRecord(response) && response.success === false) {
    throw createServerError(response)
  }
  return response
}
