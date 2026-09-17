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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import type { ApiKey, ApiKeyFormData } from '../types'

// ============================================================================
// Form Schema
// ============================================================================

export function getApiKeyFormSchema(t: TFunction) {
  // 令牌表单只描述身份、过期与访问限制（ui-spec §6.5）。
  return z.object({
    name: z.string().min(1, t('Please enter a name')),
    expired_time: z.date().optional(),
    model_limits: z.array(z.string()),
    // 只读随表单携带的拒绝清单：界面不编辑，保存时原样回写。
    deny_lanes: z.array(z.string()),
    allow_ips: z.string().optional(),
    tokenCount: z.number().min(1).optional(),
  })
}

export type ApiKeyFormValues = z.infer<ReturnType<typeof getApiKeyFormSchema>>

// ============================================================================
// Form Defaults
// ============================================================================

export const API_KEY_FORM_DEFAULT_VALUES: ApiKeyFormValues = {
  name: '',
  expired_time: undefined,
  model_limits: [],
  deny_lanes: [],
  allow_ips: '',
  tokenCount: 1,
}

export function getApiKeyFormDefaultValues(): ApiKeyFormValues {
  return {
    ...API_KEY_FORM_DEFAULT_VALUES,
  }
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload
 */
export function transformFormDataToPayload(
  data: ApiKeyFormValues
): ApiKeyFormData {
  return {
    name: data.name,
    expired_time: data.expired_time
      ? Math.floor(data.expired_time.getTime() / 1000)
      : -1,
    model_limits_enabled: data.model_limits.length > 0,
    model_limits: data.model_limits.join(','),
    // 原样保留拒绝清单（表单未提供则不额外拒绝）。
    deny_lanes: data.deny_lanes ?? [],
    allow_ips: data.allow_ips || '',
    // PBR 无用户分组：后端基座结构仍带 group 字段，固定送 'default'（design-v1 §16.9）。
    group: 'default',
  }
}

/**
 * Transform API key data to form defaults
 */
export function transformApiKeyToFormDefaults(
  apiKey: ApiKey
): ApiKeyFormValues {
  return {
    name: apiKey.name,
    expired_time:
      apiKey.expired_time > 0
        ? new Date(apiKey.expired_time * 1000)
        : undefined,
    model_limits: apiKey.model_limits
      ? apiKey.model_limits.split(',').filter(Boolean)
      : [],
    deny_lanes: apiKey.deny_lanes
      ? apiKey.deny_lanes.split(',').filter(Boolean)
      : [],
    allow_ips: apiKey.allow_ips || '',
    tokenCount: 1,
  }
}
