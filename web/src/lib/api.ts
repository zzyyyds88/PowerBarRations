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
import { api } from '@/lib/http-client'

export {
  applyAuthBundle,
  applyAuthRotation,
  bootstrapAuthentication,
  clearAuthenticatedClientState,
  clearAuthentication,
  getCommonHeaders,
  getFreshAuthHeaders,
  isAuthBundle,
  refreshAuthentication,
  resolveAuthentication,
  AuthRotationError,
} from '@/lib/auth-session'
export type { AuthTokenRotation, RefreshOutcome } from '@/lib/auth-session'
export { api }
export type { ApiRequestConfig } from '@/lib/http-client'

// ============================================================================
// User APIs
// ============================================================================

/** 路由键列表（api-spec §5.7：\`GET /api/models\` 返回 \`{items,next_cursor}\`）。 */
export async function getUserModels(): Promise<string[]> {
  // PBR 无"用户模型"概念：可用模型即全部路由键（api-spec §5.7）。
  const res = await api.get('/api/models')
  const items = (res.data as { items?: Array<{ model?: string }> }).items ?? []
  return items
    .map((item) => item.model)
    .filter(
      (name): name is string => typeof name === 'string' && name.length > 0
    )
}

// ============================================================================
// System APIs
// ============================================================================

/** \`GET /api/status\` 成功即裸状态对象（api-spec §2.3）。 */
export async function getStatus(): Promise<Record<string, unknown>> {
  const res = await api.get('/api/status')
  return (res.data ?? {}) as Record<string, unknown>
}
