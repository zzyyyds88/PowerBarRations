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
export function sendToFluent(apiKey: string, serverAddress?: string): boolean {
  if (typeof window === 'undefined') {
    return false
  }

  // 容器 id 是与 Fluent 宿主约定的注入点：优先新 id，同时兼容历史 id，
  // 避免宿主仍注入旧 id 时 prefill 静默失效。
  const container =
    document.querySelector('#fluent-pbr-container') ??
    document.querySelector('#fluent-new-api-container')
  if (!container) {
    return false
  }

  const payload = {
    id: 'pbr',
    baseUrl: serverAddress || window.location.origin,
    apiKey:
      apiKey.startsWith('sk-') || apiKey.startsWith('pbr-')
        ? apiKey
        : `sk-${apiKey}`,
  }

  container.dispatchEvent(
    new CustomEvent('fluent:prefill', {
      detail: payload,
    })
  )

  return true
}
