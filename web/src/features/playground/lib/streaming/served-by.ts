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
/**
 * 读取模型面响应头 X-Served-By（ui-spec §6.8：试打台展示实际命中的上游）。
 *
 * 两种数据源共用：
 *   - 非流式：fetch 的 Response.headers.get("X-Served-By")；
 *   - 流式：sse.js 在收到响应头时派发的 open 事件，其 headers 是
 *     "小写头名 → 值数组" 的映射（见 sse.js 的 _onReadyStateChange）。
 *
 * 头缺失或值为空时返回 undefined，展示层据此不渲染（不得伪造）。
 */
export const SERVED_BY_HEADER = 'X-Served-By'

/** 从 fetch Response 读取 X-Served-By。 */
export function servedByFromResponse(response: Response): string | undefined {
  return normalizeServedBy(response.headers.get(SERVED_BY_HEADER))
}

/**
 * 从 sse.js open 事件的 headers 读取 X-Served-By。
 * sse.js 把响应头归一为小写键、值为数组；这里兼容 string 与 string[] 两种形态。
 */
export function servedByFromStreamHeaders(
  headers: Record<string, string | string[]> | undefined
): string | undefined {
  if (!headers) return undefined
  const raw = headers[SERVED_BY_HEADER.toLowerCase()]
  if (Array.isArray(raw)) return normalizeServedBy(raw[0])
  return normalizeServedBy(raw)
}

/** 去空白；空串视作缺失。 */
export function normalizeServedBy(
  value: string | null | undefined
): string | undefined {
  const trimmed = value?.trim()
  return trimmed ? trimmed : undefined
}
