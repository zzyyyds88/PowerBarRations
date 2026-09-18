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
import { z } from 'zod'

// ============================================================================
// API Key Schema & Types
// ============================================================================

export const apiKeySchema = z.object({
  id: z.number(),
  name: z.string(),
  key: z.string(),
  // 入库明文（token-spec §3.3）：随时可读可复制。历史密钥（明文存储上线前
  // 创建）无明文可回显，此字段为空串，展示回退到 key（前缀）。
  key_plain: z.string().nullish().default(''),
  status: z.number(), // 1: enabled, 2: disabled, 3: expired, 4: exhausted
  // 只读统计：该令牌的上游折算花费（元），来自 GET /api/keys（api-spec §5.4）。
  // cost 不参与任何鉴权或限额。
  cost: z.number(),
  expired_time: z.number(), // -1 for never expires
  created_time: z.number(),
  accessed_time: z.number(),
  model_limits_enabled: z.boolean(),
  model_limits: z.string().nullish().default(''),
  // PBR lane_policy.deny_lanes：在「允许全部（或 allow 清单）」基础上叠加拒绝的
  // 路由键。列表用它区分「全部允许」与「全部允许但拒绝 X」，编辑保存时原样回填。
  deny_lanes: z.string().nullish().default(''),
  allow_ips: z.string().nullish().default(''),
})

export type ApiKey = z.infer<typeof apiKeySchema>

// ============================================================================
// API Request/Response Types
// ============================================================================

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface GetApiKeysParams {
  p?: number
  size?: number
}

export interface GetApiKeysResponse {
  success: boolean
  message?: string
  data?: {
    items: ApiKey[]
    total: number
    page: number
    page_size: number
  }
}

export interface SearchApiKeysParams {
  keyword?: string
  token?: string
  p?: number
  size?: number
}

export interface ApiKeyFormData {
  name: string
  expired_time: number
  model_limits_enabled: boolean
  model_limits: string
  /** lane_policy.deny_lanes：表单不编辑，保存时原样回写，避免静默清空。 */
  deny_lanes?: string[]
  allow_ips: string
  // PBR 无用户分组；后端基座结构仍带 group 字段，固定送 'default'。
  group: string
}

// ============================================================================
// Dialog Types
// ============================================================================

export type ApiKeysDialogType =
  | 'create'
  | 'update'
  | 'delete'
  | 'batch-delete'
  | 'cc-switch'
