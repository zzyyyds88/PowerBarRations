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
import { api } from '@/lib/api'

import type {
  GetModelsParams,
  GetModelsResponse,
  GetModelResponse,
  Model,
  SearchModelsParams,
  SyncUpstreamResponse,
  PreviewUpstreamDiffResponse,
  MissingModelsResponse,
  PrefillGroupsResponse,
  SyncLocale,
  SyncSource,
  MetadataSyncRequest,
} from './types'

// ============================================================================
// Model CRUD Operations
// ============================================================================

/**
 * Get paginated list of models
 */
export async function getModels(
  params: GetModelsParams = {}
): Promise<GetModelsResponse> {
  const res = await api.get('/api/console/models/', { params })
  return res.data
}

/**
 * Search models with filters
 */
export async function searchModels(
  params: SearchModelsParams
): Promise<GetModelsResponse> {
  const res = await api.get('/api/console/models/search', { params })
  return res.data
}

/**
 * Get single model by ID
 */
export async function getModel(id: number): Promise<GetModelResponse> {
  const res = await api.get(`/api/console/models/${id}`)
  return res.data
}

/**
 * Create new model
 */
export async function createModel(
  data: Partial<Model>
): Promise<{ success: boolean; message?: string; data?: Model }> {
  const res = await api.post('/api/console/models/', data, {
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  return res.data
}

/**
 * Update existing model
 */
export async function updateModel(
  data: Partial<Model> & { id: number }
): Promise<{ success: boolean; message?: string; data?: Model }> {
  const res = await api.put('/api/console/models/', data, {
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  return res.data
}

/**
 * Update model status only
 */
export async function updateModelStatus(
  id: number,
  status: number
): Promise<{ success: boolean; message?: string }> {
  const res = await api.put('/api/console/models/?status_only=true', { id, status })
  return res.data
}

/**
 * Delete model
 */
export async function deleteModel(
  id: number,
  removeFromChannels = false,
  removePricing = false
): Promise<{ success: boolean; message?: string; data: ModelDeleteResult }> {
  const res = await api.delete(`/api/console/models/${id}`, {
    params: {
      remove_from_channels: removeFromChannels,
      remove_pricing: removePricing,
    },
  })
  return res.data
}

// ============================================================================
// Sync Operations
// ============================================================================

/**
 * Sync upstream models (missing only or with overwrite)
 */
export async function syncUpstream(
  params: MetadataSyncRequest
): Promise<SyncUpstreamResponse> {
  const res = await api.post('/api/console/models/sync_upstream', params)
  return res.data
}

/**
 * Preview upstream diff
 */
export async function previewUpstreamDiff(params?: {
  locale?: SyncLocale
  source?: SyncSource
}): Promise<PreviewUpstreamDiffResponse> {
  const searchParams = new URLSearchParams()
  if (params?.locale) {
    searchParams.set('locale', params.locale)
  }
  if (params?.source) {
    searchParams.set('source', params.source)
  }
  const queryString = searchParams.toString()
  const url = queryString
    ? `/api/console/models/sync_upstream/preview?${queryString}`
    : '/api/console/models/sync_upstream/preview'
  const res = await api.get(url)
  return res.data
}

// ============================================================================
// Utility Operations
// ============================================================================

/**
 * Get missing models (used but not configured)
 */
export async function getMissingModels(): Promise<MissingModelsResponse> {
  const res = await api.get('/api/console/models/missing')
  return res.data
}

/**
 * Get prefill groups
 */
export async function getPrefillGroups(
  type?: 'model' | 'tag' | 'endpoint'
): Promise<PrefillGroupsResponse> {
  const res = await api.get('/api/prefill_group', {
    params: type ? { type } : undefined,
  })
  return res.data
}

/**
 * Create prefill group
 */
export async function createPrefillGroup(data: {
  name: string
  type: 'model' | 'tag' | 'endpoint'
  items: string | string[]
  description?: string
}): Promise<{ success: boolean; message?: string }> {
  const res = await api.post('/api/prefill_group', data)
  return res.data
}

/**
 * Update prefill group
 */
export async function updatePrefillGroup(data: {
  id: number
  type?: 'model' | 'tag' | 'endpoint'
  name?: string
  items?: string | string[]
  description?: string
}): Promise<{ success: boolean; message?: string }> {
  const res = await api.put('/api/prefill_group', data)
  return res.data
}

/**
 * Delete prefill group
 */
export async function deletePrefillGroup(
  id: number
): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(`/api/prefill_group/${id}`)
  return res.data
}

export interface ModelDeleteResult {
  deleted_count: number
  updated_channels: number
}

export async function deleteModels(
  modelIds: number[],
  removeFromChannels = false,
  removePricing = false
): Promise<{ success: boolean; message?: string; data: ModelDeleteResult }> {
  const res = await api.post('/api/console/models/delete', {
    model_ids: modelIds,
    remove_from_channels: removeFromChannels,
    remove_pricing: removePricing,
  })
  return res.data
}
