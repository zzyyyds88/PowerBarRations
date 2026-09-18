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
// Model Types
// ============================================================================

/**
 * Bound channel information
 */
export interface BoundChannel {
  name: string
  type: number
}

/**
 * Model entity from API
 */
export interface Model {
  has_metadata?: boolean
  configured_channel_count?: number
  id: number
  model_name: string
  description?: string
  icon?: string
  tags?: string
  endpoints?: string
  supported_endpoints?: string[]
  status: number
  sync_official: number
  created_time: number
  updated_time: number
  name_rule: number
  // Runtime fields
  bound_channels?: BoundChannel[]
  enable_groups?: string[]
  quota_types?: number[]
  matched_models?: string[]
  matched_count?: number
}

// ============================================================================
// API Request/Response Types
// ============================================================================

/**
 * Get models list parameters
 */
export interface GetModelsParams {
  include_channel_models?: boolean
  p?: number
  page_size?: number
  status?: string // filter by status
  sync_official?: string // filter by sync_official status
}

/**
 * Search models parameters
 */
export interface SearchModelsParams {
  include_channel_models?: boolean
  keyword?: string
  status?: string // filter by status
  sync_official?: string // filter by sync_official status
  p?: number
  page_size?: number
}

/**
 * 模型目录列表（基座面 \`GET /api/console/models/\`）：仍按 page/page_size 分页并
 * 返回 total（分页参数由请求端控制）。
 */
export interface GetModelsResponse {
  items: Model[]
  total: number
  page: number
  page_size: number
}

/** \`GET /api/console/models/:id\` 成功即裸模型对象。 */
export type GetModelResponse = Model

/**
 * Sync diff data
 */
export type MetadataSyncField =
  | 'description'
  | 'icon'
  | 'tags'
  | 'endpoints'
  | 'name_rule'
  | 'status'
export type MetadataSyncValues = {
  description: string
  icon: string
  tags: string
  endpoints: string
  name_rule: number
  status: number
}
export type MetadataSyncCandidate = {
  model_name: string
  kind: 'create' | 'update' | 'unchanged' | 'blocked' | 'missing_upstream'
  scope: 'site' | 'catalog'
  record_version: string
  fields: Array<{
    field: MetadataSyncField
    local: string | number
    upstream: string | number
  }>
  upstream?: MetadataSyncValues
}
export type MetadataSyncSource = {
  locale: SyncLocale
  models_url: string
  version: string
}
export type MetadataSyncPreview = {
  source: MetadataSyncSource
  candidates: MetadataSyncCandidate[]
}
export type MetadataSyncSelection = {
  model_name: string
  record_version: string
  create: boolean
  fields: MetadataSyncField[]
}
export type MetadataSyncRequest = {
  locale: SyncLocale
  source_version: string
  selections: MetadataSyncSelection[]
}
export type MetadataSyncResult = {
  created_models: string[]
  updated_models: MetadataSyncSelection[]
}
/** \`POST /api/console/models/sync_upstream\` 成功即裸同步结果。 */
export type SyncUpstreamResponse = MetadataSyncResult

/** \`GET /api/console/models/sync_upstream/preview\` 成功即裸预览。 */
export type PreviewUpstreamDiffResponse = MetadataSyncPreview

/** \`GET /api/console/models/missing\` 成功即裸模型名数组。 */
export type MissingModelsResponse = string[]

// ============================================================================
// Form Data Types
// ============================================================================

/**
 * Model form schema
 */
export const modelFormSchema = z.object({
  id: z.number().optional(),
  model_name: z.string().min(1, 'Model name is required'),
  description: z.string().default(''),
  icon: z.string().default(''),
  tags: z.array(z.string()).default([]),
  endpoints: z.string().default(''),
  name_rule: z.number().min(0).max(3).default(0),
  status: z.boolean().default(true),
  sync_official: z.boolean().default(true),
})

export type ModelFormValues = z.infer<typeof modelFormSchema>

// ============================================================================
// Utility Types
// ============================================================================

/**
 * Name rule type
 */
export type NameRule = 0 | 1 | 2 | 3 // exact, prefix, contains, suffix

/**
 * Model status type
 */
export type ModelStatus = 0 | 1 // disabled, enabled

/**
 * Quota type
 */
export type QuotaType = 0 | 1 // usage-based, per-call

/**
 * Sync locale
 */
export type SyncLocale = 'zh' | 'zh-CN' | 'en' | 'ja'

/**
 * Sync upstream source
 */
export type SyncSource = 'official'
