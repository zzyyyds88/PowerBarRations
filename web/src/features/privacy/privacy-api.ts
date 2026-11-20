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

import type { PBRRequestLogListItem } from '../usage-logs/pbr/pbr-logs-api'

export interface PrivacyRule {
  kind: string
  prefix?: string
  regex?: string
}

export interface PrivacyModelSettings {
  enabled: boolean
  required: boolean
  min_score: number
}

export interface PrivacySettings {
  secrets: boolean
  personal: boolean
  kinds: Record<string, boolean>
  words: string[]
  words_any_case: boolean
  words_whole: boolean
  rules: PrivacyRule[]
  actions: Record<string, string>
  allow: string[]
  shape: boolean
  model: PrivacyModelSettings
  active: boolean
}

export interface PrivacyHit {
  kind: string
  by: string
  placeholder: string
  head?: string
  len: number
  n: number
  action?: string
}

export interface PrivacyModelStatus {
  name: string
  revision: string
  size: number
  installed: boolean
  downloading: boolean
  done: number
  total: number
  error?: string
  loaded: boolean
  device?: string
  load_ms?: number
  min_score: number
  required: boolean
}

export interface PrivacyPreview {
  text: string
  hits: PrivacyHit[]
  active: boolean
  truncated: boolean
}

export interface PrivacyRecord extends PBRRequestLogListItem {
  mask_hits: PrivacyHit[]
}

export interface PrivacyRecordsResponse {
  items: PrivacyRecord[]
  total: number
  page: number
  page_size: number
}

export interface PrivacySummary {
  days: number
  masked_values: number
  requests: number
  blocked: number
  by_kind: { kind: string; n: number }[]
  model: PrivacyModelStatus
}

export interface PrivacyRecordsParams {
  from?: number
  to?: number
  kind?: string
  by?: string
  action?: string
  blocked?: boolean
  client?: string
  model?: string
  page?: number
  page_size?: number
}

export async function getPrivacySettings(): Promise<PrivacySettings> {
  const res = await api.get<PrivacySettings>('/api/privacy/settings')
  return res.data
}

/**
 * 发布方推荐基线（privacy-spec §4）。总开关恒为 false——它只描述
 * 「打开开关后应该长什么样」，不代表当前已启用。
 */
export async function getPrivacyRecommended(): Promise<PrivacySettings> {
  const res = await api.get<PrivacySettings>('/api/privacy/recommended')
  return res.data
}

export async function updatePrivacySettings(
  settings: PrivacySettings
): Promise<PrivacySettings> {
  const res = await api.put<PrivacySettings>('/api/privacy/settings', settings)
  return res.data
}

export async function getPrivacyModel(): Promise<PrivacyModelStatus> {
  const res = await api.get<PrivacyModelStatus>('/api/privacy/model')
  return res.data
}

export async function downloadPrivacyModel(): Promise<{
  started: boolean
  model: PrivacyModelStatus
}> {
  const res = await api.post('/api/privacy/model/download')
  return res.data as { started: boolean; model: PrivacyModelStatus }
}

export async function previewPrivacy(text: string): Promise<PrivacyPreview> {
  const res = await api.post<PrivacyPreview>('/api/privacy/preview', { text })
  return res.data
}

export async function listPrivacyRecords(
  params: PrivacyRecordsParams = {}
): Promise<PrivacyRecordsResponse> {
  const res = await api.get<PrivacyRecordsResponse>('/api/privacy/records', {
    params: {
      ...(params.from ? { from: params.from } : {}),
      ...(params.to ? { to: params.to } : {}),
      ...(params.kind ? { kind: params.kind } : {}),
      ...(params.by ? { by: params.by } : {}),
      ...(params.action ? { action: params.action } : {}),
      ...(params.blocked ? { blocked: true } : {}),
      ...(params.client ? { client: params.client } : {}),
      ...(params.model ? { model: params.model } : {}),
      page: params.page ?? 1,
      page_size: params.page_size ?? 50,
    },
  })
  return res.data
}

export async function getPrivacySummary(days = 7): Promise<PrivacySummary> {
  const res = await api.get<PrivacySummary>('/api/privacy/summary', {
    params: { days },
  })
  return res.data
}
